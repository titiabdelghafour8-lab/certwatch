// Package model defines the domain types shared by the server, the store and
// the CLI client. JSON tags define the public API wire format.
package model

import (
	"strings"
	"time"
)

// MonitorKind is what a monitor watches.
type MonitorKind string

const (
	KindTLS    MonitorKind = "tls"
	KindDomain MonitorKind = "domain"
)

// Valid reports whether k is a known monitor kind.
func (k MonitorKind) Valid() bool { return k == KindTLS || k == KindDomain }

// Status is the health of a monitor after its last check.
type Status string

const (
	StatusPending  Status = "pending"
	StatusOK       Status = "ok"
	StatusWarning  Status = "warning"
	StatusCritical Status = "critical"
	StatusExpired  Status = "expired"
	StatusError    Status = "error"
)

// ChannelKind is a notification transport.
type ChannelKind string

const (
	ChannelEmail   ChannelKind = "email"
	ChannelSlack   ChannelKind = "slack"
	ChannelDiscord ChannelKind = "discord"
	ChannelWebhook ChannelKind = "webhook"
)

// Valid reports whether k is a known channel kind.
func (k ChannelKind) Valid() bool {
	switch k {
	case ChannelEmail, ChannelSlack, ChannelDiscord, ChannelWebhook:
		return true
	}
	return false
}

// DefaultWarnDays are the alert thresholds used when a monitor specifies none.
var DefaultWarnDays = []int{30, 14, 7, 3, 1}

type User struct {
	ID                   int64     `json:"id"`
	Email                string    `json:"email"`
	Plan                 string    `json:"plan"`
	StripeCustomerID     string    `json:"-"`
	StripeSubscriptionID string    `json:"-"`
	CreatedAt            time.Time `json:"created_at"`
}

type APIKey struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

type Monitor struct {
	ID                  int64       `json:"id"`
	UserID              int64       `json:"-"`
	Kind                MonitorKind `json:"kind"`
	Target              string      `json:"target"`
	Label               string      `json:"label,omitempty"`
	WarnDays            []int       `json:"warn_days"`
	Paused              bool        `json:"paused"`
	Status              Status      `json:"status"`
	ExpiresAt           *time.Time  `json:"expires_at,omitempty"`
	DaysLeft            *int        `json:"days_left,omitempty"`
	Issuer              string      `json:"issuer,omitempty"`
	LastError           string      `json:"last_error,omitempty"`
	ConsecutiveFailures int         `json:"-"`
	LastCheckedAt       *time.Time  `json:"last_checked_at,omitempty"`
	NextCheckAt         time.Time   `json:"next_check_at"`
	CreatedAt           time.Time   `json:"created_at"`
}

// FillDaysLeft computes DaysLeft relative to now.
func (m *Monitor) FillDaysLeft(now time.Time) {
	if m.ExpiresAt == nil {
		m.DaysLeft = nil
		return
	}
	d := DaysUntil(*m.ExpiresAt, now)
	m.DaysLeft = &d
}

// DaysUntil returns whole days from now until t, rounded down. Negative values
// mean t is in the past.
func DaysUntil(t, now time.Time) int {
	d := t.Sub(now)
	days := int(d / (24 * time.Hour))
	if d < 0 && d%(24*time.Hour) != 0 {
		days-- // floor for negative durations
	}
	return days
}

// Describe returns a short human description such as "TLS certificate for example.com:443".
func (m Monitor) Describe() string {
	if m.Kind == KindDomain {
		return "Domain registration for " + m.Target
	}
	return "TLS certificate for " + m.Target
}

type Channel struct {
	ID        int64       `json:"id"`
	UserID    int64       `json:"-"`
	Kind      ChannelKind `json:"kind"`
	Target    string      `json:"target"`
	Name      string      `json:"name,omitempty"`
	CreatedAt time.Time   `json:"created_at"`
}

// Masked returns a copy whose target hides secrets embedded in webhook URLs.
func (c Channel) Masked() Channel {
	if c.Kind == ChannelEmail || len(c.Target) <= 24 {
		return c
	}
	if i := strings.Index(c.Target, "://"); i >= 0 {
		rest := c.Target[i+3:]
		if j := strings.Index(rest, "/"); j >= 0 {
			c.Target = c.Target[:i+3+j] + "/…" + c.Target[len(c.Target)-4:]
			return c
		}
	}
	c.Target = c.Target[:8] + "…" + c.Target[len(c.Target)-4:]
	return c
}

// ---- API request / response payloads ----

type SignupRequest struct {
	Email string `json:"email"`
}

type SignupResponse struct {
	User    *User  `json:"user,omitempty"`
	APIKey  string `json:"api_key,omitempty"`
	Message string `json:"message"`
}

type RecoverRequest struct {
	Email string `json:"email"`
}

type MessageResponse struct {
	Message string `json:"message"`
}

type PlanInfo struct {
	Name        string `json:"name"`
	MaxMonitors int    `json:"max_monitors"`
	MaxChannels int    `json:"max_channels"`
}

type Usage struct {
	Monitors int `json:"monitors"`
	Channels int `json:"channels"`
}

type MeResponse struct {
	User           User     `json:"user"`
	Plan           PlanInfo `json:"plan"`
	Usage          Usage    `json:"usage"`
	BillingEnabled bool     `json:"billing_enabled"`
}

type CreateMonitorRequest struct {
	Kind     MonitorKind `json:"kind"`
	Target   string      `json:"target"`
	Label    string      `json:"label,omitempty"`
	WarnDays []int       `json:"warn_days,omitempty"`
}

type CreateChannelRequest struct {
	Kind   ChannelKind `json:"kind"`
	Target string      `json:"target"`
	Name   string      `json:"name,omitempty"`
}

type CreateKeyRequest struct {
	Name string `json:"name"`
}

type CreateKeyResponse struct {
	Key    APIKey `json:"key"`
	Secret string `json:"secret"`
}

type CheckoutRequest struct {
	Plan string `json:"plan"`
}

type URLResponse struct {
	URL string `json:"url"`
}

// List wraps collection responses so they can grow pagination fields later
// without breaking clients.
type List[T any] struct {
	Items []T `json:"items"`
}

type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}
