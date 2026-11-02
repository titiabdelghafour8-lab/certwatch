package checker

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/titiabdelghafour8/certwatch/internal/netguard"
)

func TestNormalizeTLSTarget(t *testing.T) {
	cases := []struct {
		in, want string
		ok       bool
	}{
		{"example.com", "example.com:443", true},
		{"Example.COM.", "example.com:443", true},
		{"https://user@www.example.com:8443/path?x=1", "www.example.com:8443", true},
		{"10.0.0.1", "10.0.0.1:443", true},
		{"[::1]:8443", "[::1]:8443", true},
		{"example.com:0", "", false},
		{"exa mple.com", "", false},
		{"", "", false},
		{"-bad-.com", "", false},
	}
	for _, c := range cases {
		got, err := NormalizeTLSTarget(c.in, 443)
		if (err == nil) != c.ok || got != c.want {
			t.Errorf("NormalizeTLSTarget(%q) = %q, %v; want %q ok=%v", c.in, got, err, c.want, c.ok)
		}
	}
}

func TestNormalizeDomain(t *testing.T) {
	cases := []struct {
		in, want string
		ok       bool
	}{
		{"example.com", "example.com", true},
		{"https://www.shop.example.co.uk/x", "example.co.uk", true},
		{"api.example.com:8443", "example.com", true},
		{"1.2.3.4", "", false},
		{"com", "", false},
		{"localhost", "", false},
	}
	for _, c := range cases {
		got, err := NormalizeDomain(c.in)
		if (err == nil) != c.ok || got != c.want {
			t.Errorf("NormalizeDomain(%q) = %q, %v; want %q ok=%v", c.in, got, err, c.want, c.ok)
		}
	}
}

func TestTLSChecker(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	target := strings.TrimPrefix(srv.URL, "https://")

	c := &TLSChecker{AllowPrivate: true, Timeout: 5 * time.Second}
	res, err := c.Check(context.Background(), target)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !res.ExpiresAt.Equal(srv.Certificate().NotAfter.UTC()) {
		t.Errorf("ExpiresAt = %v, want %v", res.ExpiresAt, srv.Certificate().NotAfter)
	}
	if res.VerifyError == "" {
		t.Error("expected a verification error for the self-signed test certificate")
	}

	guarded := &TLSChecker{Timeout: 5 * time.Second}
	if _, err := guarded.Check(context.Background(), target); !errors.Is(err, netguard.ErrBlocked) {
		t.Errorf("guarded checker: expected ErrBlocked, got %v", err)
	}
}

func TestDomainChecker(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/dns.json":
			fmt.Fprintf(w, `{"services":[[["com","net"],["%s/rdap/"]],[["co.uk"],["%s/uk/"]]]}`, srv.URL, srv.URL)
		case "/rdap/domain/example.com":
			fmt.Fprint(w, `{
				"ldhName":"EXAMPLE.COM",
				"events":[{"eventAction":"registration","eventDate":"1995-08-14T04:00:00Z"},
				          {"eventAction":"expiration","eventDate":"2030-08-13T04:00:00Z"}],
				"entities":[{"roles":["registrar"],"vcardArray":["vcard",[["version",{},"text","4.0"],["fn",{},"text","RESERVED-IANA"]]]}]}`)
		case "/rdap/domain/noexpiry.com":
			fmt.Fprint(w, `{"events":[]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := &DomainChecker{HTTP: srv.Client(), BootstrapURL: srv.URL + "/dns.json"}
	ctx := context.Background()

	res, err := c.Check(ctx, "example.com")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if want := time.Date(2030, 8, 13, 4, 0, 0, 0, time.UTC); !res.ExpiresAt.Equal(want) {
		t.Errorf("ExpiresAt = %v, want %v", res.ExpiresAt, want)
	}
	if res.Issuer != "RESERVED-IANA" {
		t.Errorf("Issuer = %q", res.Issuer)
	}

	if _, err := c.Check(ctx, "missing.com"); !errors.Is(err, ErrNotRegistered) {
		t.Errorf("missing.com: got %v, want ErrNotRegistered", err)
	}
	if _, err := c.Check(ctx, "noexpiry.com"); !errors.Is(err, ErrNoExpiry) {
		t.Errorf("noexpiry.com: got %v, want ErrNoExpiry", err)
	}
	if _, err := c.Check(ctx, "example.zz"); !errors.Is(err, ErrNoRDAP) {
		t.Errorf("example.zz: got %v, want ErrNoRDAP", err)
	}
}
