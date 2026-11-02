// Package checker looks up expiry dates: TLS certificates by handshaking with
// the host, and domain registrations through RDAP.
package checker

import (
	"context"
	"time"

	"github.com/titiabdelghafour8/certwatch/internal/model"
)

// Result is the outcome of a successful lookup.
type Result struct {
	Kind      model.MonitorKind `json:"kind"`
	Target    string            `json:"target"`
	ExpiresAt time.Time         `json:"expires_at"`
	// Issuer is the certificate issuer (TLS) or the registrar (domain).
	Issuer  string   `json:"issuer,omitempty"`
	Subject string   `json:"subject,omitempty"`
	Names   []string `json:"names,omitempty"`
	// VerifyError is set when a TLS certificate was retrieved but does not
	// validate (untrusted chain, hostname mismatch, expired...).
	VerifyError string    `json:"verify_error,omitempty"`
	CheckedAt   time.Time `json:"checked_at"`
}

// Checker looks up the expiry of a single, already normalized target.
type Checker interface {
	Check(ctx context.Context, target string) (Result, error)
}
