package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/titiabdelghafour8/certwatch/internal/model"
)

func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	root := NewRootCmd(&out, &errOut)
	root.SetArgs(append(args, "--config", filepath.Join(t.TempDir(), "config.json")))
	err := root.Execute()
	return out.String(), err
}

func TestLocalCheckJSON(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "https://")

	out, err := run(t, "check", host, "--tls-only", "--json")
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	var results []localResult
	if err := json.Unmarshal([]byte(out), &results); err != nil {
		t.Fatalf("invalid JSON %q: %v", out, err)
	}
	if len(results) != 1 || results[0].DaysLeft == nil || results[0].Kind != model.KindTLS {
		t.Fatalf("unexpected results: %+v", results)
	}

	// The httptest certificate is valid for years, so --fail-under 100000 must fail.
	_, err = run(t, "check", host, "--tls-only", "--fail-under", "100000")
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 1 {
		t.Fatalf("expected exit code 1, got %v", err)
	}
}

func TestLocalCheckUnreachable(t *testing.T) {
	_, err := run(t, "check", "127.0.0.1:1", "--tls-only", "--timeout", "1s")
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 2 {
		t.Fatalf("expected exit code 2, got %v", err)
	}
}

func TestRequiresLogin(t *testing.T) {
	t.Setenv("CERTWATCH_API_KEY", "")
	_, err := run(t, "list")
	if err == nil || !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("expected not logged in error, got %v", err)
	}
}

func TestListAgainstFakeServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer cw_test" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"code":"unauthorized","message":"invalid or revoked API key"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"items":[{"id":1,"kind":"tls","target":"example.com:443","status":"ok","warn_days":[30],"days_left":42}]}`))
	}))
	defer srv.Close()

	out, err := run(t, "list", "--api-url", srv.URL, "--api-key", "cw_test")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "example.com:443") || !strings.Contains(out, "42") {
		t.Errorf("unexpected output:\n%s", out)
	}

	_, err = run(t, "list", "--api-url", srv.URL, "--api-key", "cw_bad")
	if err == nil || !strings.Contains(err.Error(), "invalid or revoked") {
		t.Errorf("expected API error message, got %v", err)
	}
}
