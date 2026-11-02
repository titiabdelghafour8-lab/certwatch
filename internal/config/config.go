// Package config loads server configuration from environment variables
// (12-factor style). Every variable is documented in docs/configuration.md.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/titiabdelghafour8-lab/certwatch/internal/plans"
)

type Config struct {
	DatabaseURL string
	ListenAddr  string
	PublicURL   string
	LogLevel    string
	LogFormat   string
	AutoMigrate bool
	TrustProxy  bool

	SignupEnabled         bool
	DefaultPlan           string
	AllowPrivateTargets   bool
	RestrictEmailChannels bool
	MetricsToken          string

	Worker WorkerConfig
	SMTP   SMTPConfig
	Stripe StripeConfig

	RDAPBootstrapURL string
}

type WorkerConfig struct {
	Concurrency      int
	BatchSize        int
	PollInterval     time.Duration
	TLSInterval      time.Duration
	DomainInterval   time.Duration
	ErrorRetry       time.Duration
	FailureThreshold int
}

type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
}

// Enabled reports whether outbound email is configured.
func (s SMTPConfig) Enabled() bool { return s.Host != "" && s.From != "" }

type StripeConfig struct {
	SecretKey     string
	WebhookSecret string
	// Prices maps plan name -> Stripe price ID.
	Prices map[string]string
}

// Enabled reports whether Stripe billing is configured.
func (s StripeConfig) Enabled() bool { return s.SecretKey != "" && len(s.Prices) > 0 }

// Load reads configuration from the environment and validates it.
func Load() (Config, error) {
	e := &envReader{}
	c := Config{
		DatabaseURL: e.str("CERTWATCH_DATABASE_URL", ""),
		ListenAddr:  e.str("CERTWATCH_LISTEN_ADDR", ":8080"),
		PublicURL:   strings.TrimSuffix(e.str("CERTWATCH_PUBLIC_URL", "http://localhost:8080"), "/"),
		LogLevel:    e.str("CERTWATCH_LOG_LEVEL", "info"),
		LogFormat:   e.str("CERTWATCH_LOG_FORMAT", "json"),
		AutoMigrate: e.boolean("CERTWATCH_AUTO_MIGRATE", true),
		TrustProxy:  e.boolean("CERTWATCH_TRUST_PROXY", false),

		SignupEnabled:         e.boolean("CERTWATCH_SIGNUP_ENABLED", true),
		DefaultPlan:           e.str("CERTWATCH_DEFAULT_PLAN", plans.Free.Name),
		AllowPrivateTargets:   e.boolean("CERTWATCH_ALLOW_PRIVATE_TARGETS", false),
		RestrictEmailChannels: e.boolean("CERTWATCH_RESTRICT_EMAIL_CHANNELS", true),
		MetricsToken:          e.str("CERTWATCH_METRICS_TOKEN", ""),

		Worker: WorkerConfig{
			Concurrency:      e.integer("CERTWATCH_WORKER_CONCURRENCY", 20),
			BatchSize:        e.integer("CERTWATCH_WORKER_BATCH_SIZE", 100),
			PollInterval:     e.duration("CERTWATCH_WORKER_POLL_INTERVAL", 15*time.Second),
			TLSInterval:      e.duration("CERTWATCH_TLS_CHECK_INTERVAL", 6*time.Hour),
			DomainInterval:   e.duration("CERTWATCH_DOMAIN_CHECK_INTERVAL", 24*time.Hour),
			ErrorRetry:       e.duration("CERTWATCH_ERROR_RETRY_INTERVAL", 30*time.Minute),
			FailureThreshold: e.integer("CERTWATCH_FAILURE_ALERT_THRESHOLD", 3),
		},
		SMTP: SMTPConfig{
			Host:     e.str("CERTWATCH_SMTP_HOST", ""),
			Port:     e.integer("CERTWATCH_SMTP_PORT", 587),
			Username: e.str("CERTWATCH_SMTP_USERNAME", ""),
			Password: e.str("CERTWATCH_SMTP_PASSWORD", ""),
			From:     e.str("CERTWATCH_SMTP_FROM", ""),
		},
		Stripe: StripeConfig{
			SecretKey:     e.str("CERTWATCH_STRIPE_SECRET_KEY", ""),
			WebhookSecret: e.str("CERTWATCH_STRIPE_WEBHOOK_SECRET", ""),
			Prices:        map[string]string{},
		},
		RDAPBootstrapURL: e.str("CERTWATCH_RDAP_BOOTSTRAP_URL", ""),
	}
	for _, p := range plans.Paid {
		if id := e.str("CERTWATCH_STRIPE_PRICE_"+strings.ToUpper(p.Name), ""); id != "" {
			c.Stripe.Prices[p.Name] = id
		}
	}

	if e.err != nil {
		return Config{}, e.err
	}
	return c, c.validate()
}

func (c Config) validate() error {
	var errs []error
	if c.DatabaseURL == "" {
		errs = append(errs, errors.New("CERTWATCH_DATABASE_URL is required"))
	}
	if !plans.Exists(c.DefaultPlan) {
		errs = append(errs, fmt.Errorf("CERTWATCH_DEFAULT_PLAN: unknown plan %q", c.DefaultPlan))
	}
	if c.Worker.Concurrency < 1 || c.Worker.BatchSize < 1 || c.Worker.FailureThreshold < 1 {
		errs = append(errs, errors.New("worker concurrency, batch size and failure threshold must be >= 1"))
	}
	if c.Stripe.SecretKey != "" && c.Stripe.WebhookSecret == "" {
		errs = append(errs, errors.New("CERTWATCH_STRIPE_WEBHOOK_SECRET is required when Stripe is enabled"))
	}
	if c.LogFormat != "json" && c.LogFormat != "text" {
		errs = append(errs, fmt.Errorf("CERTWATCH_LOG_FORMAT must be json or text, got %q", c.LogFormat))
	}
	return errors.Join(errs...)
}

// envReader collects the first parse error so Load can report it.
type envReader struct{ err error }

func (e *envReader) str(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return def
}

func (e *envReader) boolean(key string, def bool) bool {
	v := e.str(key, "")
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		e.fail(key, v, err)
		return def
	}
	return b
}

func (e *envReader) integer(key string, def int) int {
	v := e.str(key, "")
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		e.fail(key, v, err)
		return def
	}
	return n
}

func (e *envReader) duration(key string, def time.Duration) time.Duration {
	v := e.str(key, "")
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		e.fail(key, v, err)
		return def
	}
	return d
}

func (e *envReader) fail(key, value string, err error) {
	if e.err == nil {
		e.err = fmt.Errorf("%s=%q: %w", key, value, err)
	}
}
