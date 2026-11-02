// Package netguard provides dialers and HTTP clients that refuse to connect to
// loopback, private, link-local and other internal addresses. The hosted
// service connects to user-supplied hosts and webhook URLs, so without this
// guard a user could probe the server's internal network (SSRF).
package netguard

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"syscall"
	"time"
)

// ErrBlocked is returned when a connection targets a disallowed address.
var ErrBlocked = errors.New("destination address is not allowed")

var blockedNets = mustCIDRs(
	"100.64.0.0/10", // carrier-grade NAT
	"192.0.0.0/24",  // IETF protocol assignments
	"198.18.0.0/15", // benchmarking
	"64:ff9b::/96",  // NAT64, can map to internal IPv4
)

// IsBlocked reports whether ip must not be dialed in guarded mode.
func IsBlocked(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() {
		return true
	}
	for _, n := range blockedNets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// Dialer returns a net.Dialer. When allowPrivate is false the dialer checks
// the resolved IP right before connecting, which also defeats DNS rebinding.
func Dialer(timeout time.Duration, allowPrivate bool) *net.Dialer {
	d := &net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}
	if !allowPrivate {
		d.Control = control
	}
	return d
}

func control(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil || IsBlocked(ip) {
		return fmt.Errorf("%w: %s", ErrBlocked, host)
	}
	return nil
}

// HTTPClient returns a client whose connections go through a guarded dialer.
// Environment proxies are ignored so they cannot be used to bypass the guard.
func HTTPClient(timeout time.Duration, allowPrivate bool) *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = nil
	tr.DialContext = Dialer(10*time.Second, allowPrivate).DialContext
	tr.MaxIdleConnsPerHost = 4
	return &http.Client{
		Timeout:   timeout,
		Transport: tr,
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("stopped after 5 redirects")
			}
			return nil
		},
	}
}

func mustCIDRs(cidrs ...string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			panic(err)
		}
		out = append(out, n)
	}
	return out
}
