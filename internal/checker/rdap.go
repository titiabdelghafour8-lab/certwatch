package checker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/titiabdelghafour8/certwatch/internal/model"
	"github.com/titiabdelghafour8/certwatch/internal/version"
)

// DefaultBootstrapURL is IANA's registry of RDAP servers per TLD.
const DefaultBootstrapURL = "https://data.iana.org/rdap/dns.json"

var (
	ErrNoRDAP        = errors.New("the registry for this TLD does not offer RDAP lookups")
	ErrNoExpiry      = errors.New("the registry does not publish an expiration date for this domain")
	ErrNotRegistered = errors.New("domain is not registered")
)

// DomainChecker looks up domain expiry dates via RDAP, the structured
// successor to WHOIS.
type DomainChecker struct {
	HTTP         *http.Client
	BootstrapURL string
	CacheTTL     time.Duration

	mu       sync.Mutex
	services map[string]string // TLD -> RDAP base URL
	fetched  time.Time
}

func (c *DomainChecker) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 20 * time.Second}
}

// Check returns the expiration date of a registrable domain.
func (c *DomainChecker) Check(ctx context.Context, domain string) (Result, error) {
	base, err := c.serverFor(ctx, domain)
	if err != nil {
		return Result{}, err
	}
	u := strings.TrimSuffix(base, "/") + "/domain/" + url.PathEscape(domain)

	var doc rdapDomain
	status, err := c.getJSON(ctx, u, &doc)
	switch {
	case status == http.StatusNotFound:
		return Result{}, ErrNotRegistered
	case status == http.StatusTooManyRequests:
		return Result{}, errors.New("RDAP server rate limited the request, will retry")
	case err != nil:
		return Result{}, fmt.Errorf("rdap lookup for %s: %w", domain, err)
	}

	expires, ok := doc.expiration()
	if !ok {
		return Result{}, ErrNoExpiry
	}
	return Result{
		Kind:      model.KindDomain,
		Target:    domain,
		ExpiresAt: expires.UTC(),
		Issuer:    doc.registrar(),
		CheckedAt: time.Now().UTC(),
	}, nil
}

func (c *DomainChecker) getJSON(ctx context.Context, u string, dst any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/rdap+json, application/json")
	req.Header.Set("User-Agent", version.UserAgent())
	resp, err := c.client().Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return resp.StatusCode, fmt.Errorf("unexpected HTTP status %d from %s", resp.StatusCode, req.URL.Host)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(dst); err != nil {
		return resp.StatusCode, fmt.Errorf("decoding response from %s: %w", req.URL.Host, err)
	}
	return resp.StatusCode, nil
}

// serverFor returns the RDAP base URL responsible for domain, using the
// longest matching suffix from the IANA bootstrap file.
func (c *DomainChecker) serverFor(ctx context.Context, domain string) (string, error) {
	services, err := c.bootstrap(ctx)
	if err != nil {
		return "", err
	}
	labels := strings.Split(domain, ".")
	for i := 1; i < len(labels); i++ {
		if base, ok := services[strings.Join(labels[i:], ".")]; ok {
			return base, nil
		}
	}
	return "", ErrNoRDAP
}

func (c *DomainChecker) bootstrap(ctx context.Context) (map[string]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ttl := c.CacheTTL
	if ttl == 0 {
		ttl = 24 * time.Hour
	}
	if c.services != nil && time.Since(c.fetched) < ttl {
		return c.services, nil
	}

	bootURL := c.BootstrapURL
	if bootURL == "" {
		bootURL = DefaultBootstrapURL
	}
	var doc struct {
		Services [][][]string `json:"services"`
	}
	if _, err := c.getJSON(ctx, bootURL, &doc); err != nil {
		if c.services != nil {
			return c.services, nil // serve stale data rather than failing every check
		}
		return nil, fmt.Errorf("loading RDAP bootstrap: %w", err)
	}

	services := make(map[string]string)
	for _, svc := range doc.Services {
		if len(svc) < 2 || len(svc[1]) == 0 {
			continue
		}
		base := svc[1][0]
		for _, u := range svc[1] {
			if strings.HasPrefix(u, "https://") {
				base = u
				break
			}
		}
		for _, tld := range svc[0] {
			services[strings.ToLower(tld)] = base
		}
	}
	c.services, c.fetched = services, time.Now()
	return services, nil
}

type rdapEvent struct {
	Action string `json:"eventAction"`
	Date   string `json:"eventDate"`
}

type rdapEntity struct {
	Roles    []string          `json:"roles"`
	VCard    []json.RawMessage `json:"vcardArray"`
	Entities []rdapEntity      `json:"entities"`
}

type rdapDomain struct {
	Events   []rdapEvent  `json:"events"`
	Entities []rdapEntity `json:"entities"`
}

func (d rdapDomain) expiration() (time.Time, bool) {
	for _, ev := range d.Events {
		if strings.EqualFold(ev.Action, "expiration") || strings.EqualFold(ev.Action, "registration expiration") {
			for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02"} {
				if t, err := time.Parse(layout, ev.Date); err == nil {
					return t, true
				}
			}
		}
	}
	return time.Time{}, false
}

func (d rdapDomain) registrar() string {
	for _, e := range d.Entities {
		for _, r := range e.Roles {
			if r == "registrar" {
				return vcardFN(e.VCard)
			}
		}
	}
	return ""
}

// vcardFN extracts the "fn" (formatted name) property from a jCard array:
// ["vcard", [["version", {}, "text", "4.0"], ["fn", {}, "text", "Name"]]]
func vcardFN(vcard []json.RawMessage) string {
	if len(vcard) < 2 {
		return ""
	}
	var props [][]any
	if err := json.Unmarshal(vcard[1], &props); err != nil {
		return ""
	}
	for _, p := range props {
		if len(p) >= 4 {
			if name, _ := p[0].(string); name == "fn" {
				v, _ := p[3].(string)
				return v
			}
		}
	}
	return ""
}
