package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/titiabdelghafour8-lab/certwatch/internal/billing"
	"github.com/titiabdelghafour8-lab/certwatch/internal/config"
	"github.com/titiabdelghafour8-lab/certwatch/internal/metrics"
	"github.com/titiabdelghafour8-lab/certwatch/internal/model"
	"github.com/titiabdelghafour8-lab/certwatch/internal/notify"
	"github.com/titiabdelghafour8-lab/certwatch/internal/store"
)

// These are integration tests against a real PostgreSQL database. They run
// when CERTWATCH_TEST_DATABASE_URL is set (CI and `make test-integration`).

type stubChecker struct{}

func (stubChecker) Process(_ context.Context, m model.Monitor) model.Monitor {
	exp := time.Now().Add(40 * 24 * time.Hour)
	m.ExpiresAt, m.Status = &exp, model.StatusOK
	return m
}

type stubNotifier struct{ sent int }

func (n *stubNotifier) Send(context.Context, model.Channel, notify.Message) error {
	n.sent++
	return nil
}
func (n *stubNotifier) SendEmail(context.Context, string, string, string) error { return nil }
func (n *stubNotifier) EmailEnabled() bool                                      { return false }

type testEnv struct {
	t   *testing.T
	srv *httptest.Server
	st  *store.Store
}

func setup(t *testing.T, mutate func(*config.Config)) *testEnv {
	t.Helper()
	dsn := os.Getenv("CERTWATCH_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("CERTWATCH_TEST_DATABASE_URL not set; skipping integration test")
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := context.Background()
	st, err := store.Open(ctx, dsn, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := st.Exec(ctx, "TRUNCATE users RESTART IDENTITY CASCADE"); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{SignupEnabled: true, DefaultPlan: "free", RestrictEmailChannels: true, PublicURL: "http://test"}
	if mutate != nil {
		mutate(&cfg)
	}
	b := billing.NewStripe(cfg.Stripe, cfg.PublicURL)
	s := New(cfg, st, stubChecker{}, &stubNotifier{}, b, log, metrics.New())
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	return &testEnv{t: t, srv: srv, st: st}
}

func (e *testEnv) do(method, path, key string, body any, out any) int {
	e.t.Helper()
	var buf io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		buf = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, buf)
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil {
		_ = json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode
}

func (e *testEnv) signup(email string) string {
	e.t.Helper()
	var res model.SignupResponse
	if code := e.do("POST", "/v1/signup", "", model.SignupRequest{Email: email}, &res); code != http.StatusCreated {
		e.t.Fatalf("signup: HTTP %d", code)
	}
	if !strings.HasPrefix(res.APIKey, "cw_") {
		e.t.Fatalf("unexpected key %q", res.APIKey)
	}
	return res.APIKey
}

func TestSignupAndAuth(t *testing.T) {
	e := setup(t, nil)
	key := e.signup("Alice@Example.com")

	if code := e.do("POST", "/v1/signup", "", model.SignupRequest{Email: "alice@example.com"}, nil); code != http.StatusConflict {
		t.Errorf("duplicate signup: HTTP %d", code)
	}
	if code := e.do("POST", "/v1/signup", "", model.SignupRequest{Email: "not-an-email"}, nil); code != http.StatusBadRequest {
		t.Errorf("bad email: HTTP %d", code)
	}
	if code := e.do("GET", "/v1/me", "", nil, nil); code != http.StatusUnauthorized {
		t.Errorf("no key: HTTP %d", code)
	}
	if code := e.do("GET", "/v1/me", "cw_wrong", nil, nil); code != http.StatusUnauthorized {
		t.Errorf("wrong key: HTTP %d", code)
	}

	var me model.MeResponse
	if code := e.do("GET", "/v1/me", key, nil, &me); code != http.StatusOK {
		t.Fatalf("me: HTTP %d", code)
	}
	if me.User.Email != "alice@example.com" || me.Plan.Name != "free" || me.Plan.MaxMonitors != 5 {
		t.Errorf("unexpected me: %+v", me)
	}
}

func TestMonitorsLifecycleAndLimits(t *testing.T) {
	e := setup(t, nil)
	key := e.signup("bob@example.com")
	other := e.signup("eve@example.com")

	var m model.Monitor
	code := e.do("POST", "/v1/monitors", key, model.CreateMonitorRequest{Kind: "tls", Target: "https://Example.com/path"}, &m)
	if code != http.StatusCreated || m.Target != "example.com:443" || len(m.WarnDays) != 5 {
		t.Fatalf("create: HTTP %d %+v", code, m)
	}
	if code := e.do("POST", "/v1/monitors", key, model.CreateMonitorRequest{Kind: "tls", Target: "example.com"}, nil); code != http.StatusConflict {
		t.Errorf("duplicate: HTTP %d", code)
	}
	if code := e.do("POST", "/v1/monitors", key, model.CreateMonitorRequest{Kind: "dns", Target: "example.com"}, nil); code != http.StatusBadRequest {
		t.Errorf("bad kind: HTTP %d", code)
	}
	path := "/v1/monitors/" + itoa(m.ID)

	// Tenancy: another user cannot see or delete it.
	if code := e.do("GET", path, other, nil, nil); code != http.StatusNotFound {
		t.Errorf("cross-tenant read: HTTP %d", code)
	}
	if code := e.do("DELETE", path, other, nil, nil); code != http.StatusNotFound {
		t.Errorf("cross-tenant delete: HTTP %d", code)
	}

	var checked model.Monitor
	if code := e.do("POST", path+"/check", key, nil, &checked); code != http.StatusOK || checked.DaysLeft == nil {
		t.Errorf("check: HTTP %d %+v", code, checked)
	}
	var paused model.Monitor
	if code := e.do("POST", path+"/pause", key, nil, &paused); code != http.StatusOK || !paused.Paused {
		t.Errorf("pause: HTTP %d", code)
	}

	// Free plan allows 5 monitors.
	for _, d := range []string{"a.com", "b.com", "c.com", "d.com"} {
		if code := e.do("POST", "/v1/monitors", key, model.CreateMonitorRequest{Kind: "domain", Target: d}, nil); code != http.StatusCreated {
			t.Fatalf("create %s: HTTP %d", d, code)
		}
	}
	if code := e.do("POST", "/v1/monitors", key, model.CreateMonitorRequest{Kind: "domain", Target: "e.com"}, nil); code != http.StatusPaymentRequired {
		t.Errorf("over limit: HTTP %d", code)
	}

	var list model.List[model.Monitor]
	if code := e.do("GET", "/v1/monitors", key, nil, &list); code != http.StatusOK || len(list.Items) != 5 {
		t.Errorf("list: HTTP %d, %d items", code, len(list.Items))
	}
	if code := e.do("DELETE", path, key, nil, nil); code != http.StatusNoContent {
		t.Errorf("delete: HTTP %d", code)
	}
}

func TestChannelsValidation(t *testing.T) {
	e := setup(t, nil)
	key := e.signup("carol@example.com")

	bad := []model.CreateChannelRequest{
		{Kind: "slack", Target: "https://evil.example.com/hook"},
		{Kind: "discord", Target: "https://discord.com/not-a-webhook"},
		{Kind: "webhook", Target: "http://example.com/hook"},
		{Kind: "email", Target: "someone-else@example.com"},
		{Kind: "sms", Target: "+123"},
	}
	for _, req := range bad {
		if code := e.do("POST", "/v1/channels", key, req, nil); code != http.StatusBadRequest {
			t.Errorf("%s %s: HTTP %d, want 400", req.Kind, req.Target, code)
		}
	}

	var ch model.Channel
	req := model.CreateChannelRequest{Kind: "slack", Target: "https://hooks.slack.com/services/T000/B000/XXXXXXXXXXXXXXXX"}
	if code := e.do("POST", "/v1/channels", key, req, &ch); code != http.StatusCreated {
		t.Fatalf("create slack: HTTP %d", code)
	}
	if strings.Contains(ch.Target, "XXXXXXXX") {
		t.Errorf("channel secret not masked: %s", ch.Target)
	}
	if code := e.do("POST", "/v1/channels/"+itoa(ch.ID)+"/test", key, nil, nil); code != http.StatusOK {
		t.Errorf("test channel: HTTP %d", code)
	}
	// Free plan allows 1 channel.
	req.Target = "https://hooks.slack.com/services/T000/B000/YYYYYYYYYYYYYYYY"
	if code := e.do("POST", "/v1/channels", key, req, nil); code != http.StatusPaymentRequired {
		t.Errorf("over channel limit: HTTP %d", code)
	}
}

func TestKeys(t *testing.T) {
	e := setup(t, nil)
	key := e.signup("dave@example.com")

	var created model.CreateKeyResponse
	if code := e.do("POST", "/v1/keys", key, model.CreateKeyRequest{Name: "ci"}, &created); code != http.StatusCreated {
		t.Fatalf("create key: HTTP %d", code)
	}
	if code := e.do("GET", "/v1/me", created.Secret, nil, nil); code != http.StatusOK {
		t.Errorf("new key rejected: HTTP %d", code)
	}
	if code := e.do("DELETE", "/v1/keys/"+itoa(created.Key.ID), key, nil, nil); code != http.StatusNoContent {
		t.Errorf("revoke: HTTP %d", code)
	}
	if code := e.do("GET", "/v1/me", created.Secret, nil, nil); code != http.StatusUnauthorized {
		t.Errorf("revoked key still works: HTTP %d", code)
	}
}

func TestStripeWebhookUpgradesPlan(t *testing.T) {
	e := setup(t, func(c *config.Config) {
		c.Stripe = config.StripeConfig{SecretKey: "sk_test", WebhookSecret: "whsec_test", Prices: map[string]string{"pro": "price_pro"}}
	})
	key := e.signup("frank@example.com")
	var me model.MeResponse
	e.do("GET", "/v1/me", key, nil, &me)

	send := func(payload string) int {
		req, _ := http.NewRequest("POST", e.srv.URL+"/v1/billing/webhook", strings.NewReader(payload))
		req.Header.Set("Stripe-Signature", billing.Sign([]byte(payload), "whsec_test", time.Now()))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	completed := `{"id":"evt_1","type":"checkout.session.completed","data":{"object":{
		"client_reference_id":"` + itoa(me.User.ID) + `","customer":"cus_1","subscription":"sub_1","metadata":{"plan":"pro"}}}}`
	if code := send(completed); code != http.StatusOK {
		t.Fatalf("webhook: HTTP %d", code)
	}
	e.do("GET", "/v1/me", key, nil, &me)
	if me.Plan.Name != "pro" {
		t.Fatalf("plan after checkout = %s", me.Plan.Name)
	}

	deleted := `{"id":"evt_2","type":"customer.subscription.deleted","data":{"object":{"id":"sub_1","customer":"cus_1","status":"canceled"}}}`
	if code := send(deleted); code != http.StatusOK {
		t.Fatalf("webhook: HTTP %d", code)
	}
	e.do("GET", "/v1/me", key, nil, &me)
	if me.Plan.Name != "free" {
		t.Fatalf("plan after cancel = %s", me.Plan.Name)
	}

	req, _ := http.NewRequest("POST", e.srv.URL+"/v1/billing/webhook", strings.NewReader(completed))
	req.Header.Set("Stripe-Signature", "t=1,v1=00")
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("forged webhook: HTTP %d", resp.StatusCode)
	}
}

func TestSignupDisabled(t *testing.T) {
	e := setup(t, func(c *config.Config) { c.SignupEnabled = false })
	if code := e.do("POST", "/v1/signup", "", model.SignupRequest{Email: "x@example.com"}, nil); code != http.StatusForbidden {
		t.Errorf("HTTP %d", code)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
