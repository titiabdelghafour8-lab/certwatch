package checker

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/titiabdelghafour8/certwatch/internal/model"
	"github.com/titiabdelghafour8/certwatch/internal/netguard"
)

// TLSChecker reads the leaf certificate presented by a TLS server.
type TLSChecker struct {
	Timeout time.Duration
	// AllowPrivate permits loopback/private destinations. Keep it false on
	// multi-tenant deployments.
	AllowPrivate bool
	// RootCAs overrides the system trust store (used in tests).
	RootCAs *x509.CertPool
}

// Check handshakes with target ("host:port") and returns the leaf expiry.
func (c *TLSChecker) Check(ctx context.Context, target string) (Result, error) {
	host, _, err := net.SplitHostPort(target)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %w", ErrInvalidTarget, err)
	}
	timeout := c.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	dialer := &tls.Dialer{
		NetDialer: netguard.Dialer(timeout, c.AllowPrivate),
		Config: &tls.Config{
			ServerName: host,
			// We must read certificates that are expired or otherwise invalid,
			// so verification is done manually below.
			InsecureSkipVerify: true,             //nolint:gosec // verified manually after the handshake
			MinVersion:         tls.VersionTLS10, //nolint:gosec // we only read certificates, no data is exchanged
		},
	}
	conn, err := dialer.DialContext(ctx, "tcp", target)
	if err != nil {
		if errors.Is(err, netguard.ErrBlocked) {
			return Result{}, err
		}
		return Result{}, fmt.Errorf("tls handshake with %s failed: %w", target, err)
	}
	defer conn.Close()

	tlsConn, ok := conn.(*tls.Conn)
	if !ok {
		return Result{}, errors.New("unexpected connection type")
	}
	certs := tlsConn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return Result{}, fmt.Errorf("%s presented no certificate", target)
	}
	leaf := certs[0]

	res := Result{
		Kind:      model.KindTLS,
		Target:    target,
		ExpiresAt: leaf.NotAfter.UTC(),
		Issuer:    issuerName(leaf),
		Subject:   leaf.Subject.CommonName,
		Names:     leaf.DNSNames,
		CheckedAt: time.Now().UTC(),
	}

	intermediates := x509.NewCertPool()
	for _, ic := range certs[1:] {
		intermediates.AddCert(ic)
	}
	if _, err := leaf.Verify(x509.VerifyOptions{
		DNSName:       host,
		Intermediates: intermediates,
		Roots:         c.RootCAs,
	}); err != nil {
		res.VerifyError = err.Error()
	}
	return res, nil
}

func issuerName(c *x509.Certificate) string {
	if len(c.Issuer.Organization) > 0 {
		return c.Issuer.Organization[0]
	}
	return c.Issuer.CommonName
}
