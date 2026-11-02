// Package api implements the versioned HTTP JSON API (/v1).
package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/titiabdelghafour8/certwatch/internal/billing"
	"github.com/titiabdelghafour8/certwatch/internal/config"
	"github.com/titiabdelghafour8/certwatch/internal/metrics"
	"github.com/titiabdelghafour8/certwatch/internal/model"
	"github.com/titiabdelghafour8/certwatch/internal/notify"
	"github.com/titiabdelghafour8/certwatch/internal/ratelimit"
	"github.com/titiabdelghafour8/certwatch/internal/store"
)

// Checker runs an immediate check for "check now" requests.
type Checker interface {
	Process(ctx context.Context, m model.Monitor) model.Monitor
}

// Notifier sends alerts and account emails.
type Notifier interface {
	Send(ctx context.Context, ch model.Channel, msg notify.Message) error
	SendEmail(ctx context.Context, to, subject, body string) error
	EmailEnabled() bool
}

type Server struct {
	cfg      config.Config
	store    *store.Store
	checker  Checker
	notifier Notifier
	billing  *billing.Stripe
	log      *slog.Logger
	metrics  *metrics.Registry

	signupLimiter *ratelimit.Limiter
	apiLimiter    *ratelimit.Limiter
	now           func() time.Time
}

func New(cfg config.Config, st *store.Store, c Checker, n Notifier, b *billing.Stripe, log *slog.Logger, m *metrics.Registry) *Server {
	m.Describe("certwatch_http_requests_total", "HTTP requests, by route and status code.")
	return &Server{
		cfg:           cfg,
		store:         st,
		checker:       c,
		notifier:      n,
		billing:       b,
		log:           log,
		metrics:       m,
		signupLimiter: ratelimit.New(10*time.Minute, 5),
		apiLimiter:    ratelimit.New(100*time.Millisecond, 30),
		now:           time.Now,
	}
}

// Handler returns the root HTTP handler with all routes and middleware.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", s.handleReady)
	mux.Handle("GET /metrics", s.metrics.Handler(s.cfg.MetricsToken))
	mux.HandleFunc("GET /billing/success", staticPage("Payment received", "Your plan is now active. You can close this tab and return to your terminal: run <code>certwatch whoami</code> to confirm."))
	mux.HandleFunc("GET /billing/cancel", staticPage("Checkout cancelled", "No charge was made. Run <code>certwatch upgrade</code> whenever you are ready."))

	mux.HandleFunc("POST /v1/signup", s.handleSignup)
	mux.HandleFunc("POST /v1/recover", s.handleRecover)
	mux.HandleFunc("POST /v1/billing/webhook", s.handleStripeWebhook)

	mux.Handle("GET /v1/me", s.auth(s.handleMe))

	mux.Handle("GET /v1/keys", s.auth(s.handleListKeys))
	mux.Handle("POST /v1/keys", s.auth(s.handleCreateKey))
	mux.Handle("DELETE /v1/keys/{id}", s.auth(s.handleRevokeKey))

	mux.Handle("GET /v1/monitors", s.auth(s.handleListMonitors))
	mux.Handle("POST /v1/monitors", s.auth(s.handleCreateMonitor))
	mux.Handle("GET /v1/monitors/{id}", s.auth(s.handleGetMonitor))
	mux.Handle("DELETE /v1/monitors/{id}", s.auth(s.handleDeleteMonitor))
	mux.Handle("POST /v1/monitors/{id}/check", s.auth(s.handleCheckMonitor))
	mux.Handle("POST /v1/monitors/{id}/pause", s.auth(s.handlePauseMonitor(true)))
	mux.Handle("POST /v1/monitors/{id}/resume", s.auth(s.handlePauseMonitor(false)))

	mux.Handle("GET /v1/channels", s.auth(s.handleListChannels))
	mux.Handle("POST /v1/channels", s.auth(s.handleCreateChannel))
	mux.Handle("DELETE /v1/channels/{id}", s.auth(s.handleDeleteChannel))
	mux.Handle("POST /v1/channels/{id}/test", s.auth(s.handleTestChannel))

	mux.Handle("POST /v1/billing/checkout", s.auth(s.handleCheckout))
	mux.Handle("POST /v1/billing/portal", s.auth(s.handlePortal))

	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "no such endpoint")
	})

	return s.recoverer(s.requestLog(securityHeaders(mux)))
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.store.Ping(ctx); err != nil {
		writeError(w, http.StatusServiceUnavailable, "not_ready", "database unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func staticPage(title, body string) http.HandlerFunc {
	page := `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>` + title + ` · certwatch</title><style>body{font:16px/1.5 system-ui,sans-serif;max-width:36rem;margin:15vh auto;padding:0 1rem;color:#1a1a1a}
code{background:#f2f2f2;padding:.1rem .3rem;border-radius:4px}@media(prefers-color-scheme:dark){body{background:#111;color:#eee}code{background:#222}}</style></head>
<body><h1>` + title + `</h1><p>` + body + `</p></body></html>`
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(page))
	}
}
