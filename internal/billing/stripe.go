// Package billing integrates Stripe Checkout, the customer portal and
// subscription webhooks using Stripe's REST API directly (no SDK dependency).
package billing

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/titiabdelghafour8-lab/certwatch/internal/config"
)

const apiBase = "https://api.stripe.com"

var ErrInvalidSignature = errors.New("invalid stripe signature")

type Stripe struct {
	cfg  config.StripeConfig
	http *http.Client
	base string
	// PublicURL is where Checkout redirects back to.
	PublicURL string
}

func NewStripe(cfg config.StripeConfig, publicURL string) *Stripe {
	return &Stripe{cfg: cfg, http: &http.Client{Timeout: 20 * time.Second}, base: apiBase, PublicURL: publicURL}
}

// Enabled reports whether billing is configured.
func (s *Stripe) Enabled() bool { return s != nil && s.cfg.Enabled() }

// PriceFor returns the Stripe price ID for a plan.
func (s *Stripe) PriceFor(plan string) (string, bool) {
	id, ok := s.cfg.Prices[plan]
	return id, ok
}

// PlanForPrice maps a Stripe price ID back to a plan name.
func (s *Stripe) PlanForPrice(priceID string) (string, bool) {
	for plan, id := range s.cfg.Prices {
		if id == priceID {
			return plan, true
		}
	}
	return "", false
}

// CreateCheckout starts a subscription checkout and returns its URL.
func (s *Stripe) CreateCheckout(ctx context.Context, userID int64, email, customerID, plan string) (string, error) {
	price, ok := s.PriceFor(plan)
	if !ok {
		return "", fmt.Errorf("plan %q is not purchasable", plan)
	}
	uid := strconv.FormatInt(userID, 10)
	form := url.Values{
		"mode":                                 {"subscription"},
		"line_items[0][price]":                 {price},
		"line_items[0][quantity]":              {"1"},
		"client_reference_id":                  {uid},
		"success_url":                          {s.PublicURL + "/billing/success"},
		"cancel_url":                           {s.PublicURL + "/billing/cancel"},
		"allow_promotion_codes":                {"true"},
		"metadata[user_id]":                    {uid},
		"metadata[plan]":                       {plan},
		"subscription_data[metadata][user_id]": {uid},
		"subscription_data[metadata][plan]":    {plan},
	}
	if customerID != "" {
		form.Set("customer", customerID)
	} else {
		form.Set("customer_email", email)
	}
	var out struct {
		URL string `json:"url"`
	}
	if err := s.post(ctx, "/v1/checkout/sessions", form, &out); err != nil {
		return "", err
	}
	return out.URL, nil
}

// CreatePortal returns a customer portal URL for managing the subscription.
func (s *Stripe) CreatePortal(ctx context.Context, customerID string) (string, error) {
	var out struct {
		URL string `json:"url"`
	}
	form := url.Values{"customer": {customerID}, "return_url": {s.PublicURL}}
	if err := s.post(ctx, "/v1/billing_portal/sessions", form, &out); err != nil {
		return "", err
	}
	return out.URL, nil
}

func (s *Stripe) post(ctx context.Context, path string, form url.Values, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.base+path, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.SetBasicAuth(s.cfg.SecretKey, "")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.http.Do(req)
	if err != nil {
		return fmt.Errorf("stripe request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(body, &e)
		return fmt.Errorf("stripe %s: HTTP %d: %s", path, resp.StatusCode, e.Error.Message)
	}
	return json.Unmarshal(body, dst)
}

// Event is the subset of a Stripe webhook event that certwatch uses.
type Event struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Data struct {
		Object json.RawMessage `json:"object"`
	} `json:"data"`
}

// CheckoutSession is the object of checkout.session.completed events.
type CheckoutSession struct {
	ClientReferenceID string            `json:"client_reference_id"`
	Customer          string            `json:"customer"`
	Subscription      string            `json:"subscription"`
	Metadata          map[string]string `json:"metadata"`
}

// Subscription is the object of customer.subscription.* events.
type Subscription struct {
	ID       string `json:"id"`
	Customer string `json:"customer"`
	Status   string `json:"status"`
	Items    struct {
		Data []struct {
			Price struct {
				ID string `json:"id"`
			} `json:"price"`
		} `json:"data"`
	} `json:"items"`
}

// PriceID returns the first price on the subscription.
func (s Subscription) PriceID() string {
	if len(s.Items.Data) == 0 {
		return ""
	}
	return s.Items.Data[0].Price.ID
}

// Active reports whether the subscription currently grants access.
func (s Subscription) Active() bool {
	switch s.Status {
	case "active", "trialing", "past_due":
		return true
	}
	return false
}

// ParseWebhook verifies the Stripe-Signature header and decodes the event.
func (s *Stripe) ParseWebhook(payload []byte, sigHeader string, now time.Time) (Event, error) {
	if err := VerifySignature(payload, sigHeader, s.cfg.WebhookSecret, now, 5*time.Minute); err != nil {
		return Event{}, err
	}
	var ev Event
	if err := json.Unmarshal(payload, &ev); err != nil {
		return Event{}, fmt.Errorf("decoding event: %w", err)
	}
	return ev, nil
}

// VerifySignature implements Stripe's webhook signing scheme:
// header "t=<unix>,v1=<hex hmac>", signed payload "<t>.<body>".
func VerifySignature(payload []byte, header, secret string, now time.Time, tolerance time.Duration) error {
	if secret == "" {
		return fmt.Errorf("%w: webhook secret not configured", ErrInvalidSignature)
	}
	var ts string
	var sigs []string
	for _, part := range strings.Split(header, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		switch k {
		case "t":
			ts = v
		case "v1":
			sigs = append(sigs, v)
		}
	}
	if ts == "" || len(sigs) == 0 {
		return fmt.Errorf("%w: malformed header", ErrInvalidSignature)
	}
	unix, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return fmt.Errorf("%w: bad timestamp", ErrInvalidSignature)
	}
	if d := now.Sub(time.Unix(unix, 0)); d > tolerance || d < -tolerance {
		return fmt.Errorf("%w: timestamp outside tolerance", ErrInvalidSignature)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts))
	mac.Write([]byte("."))
	mac.Write(payload)
	expected := mac.Sum(nil)
	for _, sig := range sigs {
		if got, err := hex.DecodeString(sig); err == nil && hmac.Equal(got, expected) {
			return nil
		}
	}
	return ErrInvalidSignature
}

// Sign produces a Stripe-Signature header value (used by tests and local tooling).
func Sign(payload []byte, secret string, at time.Time) string {
	ts := strconv.FormatInt(at.Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "."))
	mac.Write(payload)
	return "t=" + ts + ",v1=" + hex.EncodeToString(mac.Sum(nil))
}
