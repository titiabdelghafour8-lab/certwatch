package config

import (
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("CERTWATCH_DATABASE_URL", "postgres://localhost/db")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.ListenAddr != ":8080" || c.Worker.TLSInterval != 6*time.Hour || c.DefaultPlan != "free" {
		t.Errorf("unexpected defaults: %+v", c)
	}
	if c.Stripe.Enabled() || c.SMTP.Enabled() {
		t.Error("stripe and smtp should be disabled by default")
	}
}

func TestLoadErrors(t *testing.T) {
	t.Setenv("CERTWATCH_DATABASE_URL", "")
	if _, err := Load(); err == nil {
		t.Error("expected error for missing database URL")
	}

	t.Setenv("CERTWATCH_DATABASE_URL", "postgres://localhost/db")
	t.Setenv("CERTWATCH_TLS_CHECK_INTERVAL", "soon")
	if _, err := Load(); err == nil {
		t.Error("expected error for bad duration")
	}
}

func TestStripePrices(t *testing.T) {
	t.Setenv("CERTWATCH_DATABASE_URL", "postgres://localhost/db")
	t.Setenv("CERTWATCH_STRIPE_SECRET_KEY", "sk_test_x")
	t.Setenv("CERTWATCH_STRIPE_WEBHOOK_SECRET", "whsec_x")
	t.Setenv("CERTWATCH_STRIPE_PRICE_PRO", "price_123")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !c.Stripe.Enabled() || c.Stripe.Prices["pro"] != "price_123" {
		t.Errorf("stripe config not loaded: %+v", c.Stripe)
	}
}
