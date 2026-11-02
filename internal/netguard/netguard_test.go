package netguard

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestIsBlocked(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1":       true,
		"10.1.2.3":        true,
		"172.16.0.1":      true,
		"192.168.1.1":     true,
		"169.254.169.254": true,
		"100.64.0.1":      true,
		"0.0.0.0":         true,
		"::1":             true,
		"fe80::1":         true,
		"fd00::1":         true,
		"8.8.8.8":         false,
		"1.1.1.1":         false,
		"2606:4700::1111": false,
	}
	for in, want := range cases {
		if got := IsBlocked(net.ParseIP(in)); got != want {
			t.Errorf("IsBlocked(%s) = %v, want %v", in, got, want)
		}
	}
}

func TestHTTPClientBlocksLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()

	_, err := HTTPClient(2*time.Second, false).Get(srv.URL)
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("expected ErrBlocked, got %v", err)
	}

	resp, err := HTTPClient(2*time.Second, true).Get(srv.URL)
	if err != nil {
		t.Fatalf("allowPrivate client failed: %v", err)
	}
	_ = resp.Body.Close()
}

func TestDialerBlocksLoopback(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	_, err = Dialer(time.Second, false).DialContext(context.Background(), "tcp", ln.Addr().String())
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("expected ErrBlocked, got %v", err)
	}
}
