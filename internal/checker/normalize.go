package checker

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/publicsuffix"
)

var hostnameRe = regexp.MustCompile(`^([a-z0-9_]([a-z0-9-]{0,61}[a-z0-9])?\.)*[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// ErrInvalidTarget is returned for inputs that are not a hostname or domain.
var ErrInvalidTarget = errors.New("invalid target")

// stripURL reduces "https://user@Example.com:8443/path?q" to "example.com:8443".
func stripURL(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	if i := strings.LastIndex(s, "@"); i >= 0 {
		s = s[i+1:]
	}
	return s
}

func validHost(host string) bool {
	if net.ParseIP(host) != nil {
		return true
	}
	return len(host) <= 253 && hostnameRe.MatchString(host)
}

// NormalizeTLSTarget turns user input into "host:port".
func NormalizeTLSTarget(input string, defaultPort int) (string, error) {
	s := stripURL(input)
	host, port := s, strconv.Itoa(defaultPort)
	if h, p, err := net.SplitHostPort(s); err == nil {
		host, port = h, p
	} else if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
		host = s[1 : len(s)-1]
	}
	host = strings.TrimSuffix(host, ".")
	if host == "" || !validHost(host) {
		return "", fmt.Errorf("%w: %q is not a valid hostname", ErrInvalidTarget, input)
	}
	p, err := strconv.Atoi(port)
	if err != nil || p < 1 || p > 65535 {
		return "", fmt.Errorf("%w: invalid port %q", ErrInvalidTarget, port)
	}
	return net.JoinHostPort(host, port), nil
}

// NormalizeDomain turns user input such as "https://www.shop.example.co.uk/x"
// into the registrable domain "example.co.uk".
func NormalizeDomain(input string) (string, error) {
	s := stripURL(input)
	if h, _, err := net.SplitHostPort(s); err == nil {
		s = h
	}
	s = strings.TrimSuffix(s, ".")
	if net.ParseIP(strings.Trim(s, "[]")) != nil {
		return "", fmt.Errorf("%w: IP addresses have no domain registration", ErrInvalidTarget)
	}
	if !validHost(s) || !strings.Contains(s, ".") {
		return "", fmt.Errorf("%w: %q is not a valid domain", ErrInvalidTarget, input)
	}
	d, err := publicsuffix.EffectiveTLDPlusOne(s)
	if err != nil {
		return "", fmt.Errorf("%w: %q is not a registrable domain", ErrInvalidTarget, input)
	}
	return d, nil
}
