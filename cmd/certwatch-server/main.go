// Command certwatch-server runs the certwatch API and/or the check worker.
//
//	certwatch-server [all|serve|worker|migrate|healthcheck|version]
//
// "all" (the default) runs the API and a worker in one process, which is the
// simplest deployment. For scale, run "serve" and "worker" as separate
// replicas against the same database.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/titiabdelghafour8-lab/certwatch/internal/api"
	"github.com/titiabdelghafour8-lab/certwatch/internal/billing"
	"github.com/titiabdelghafour8-lab/certwatch/internal/checker"
	"github.com/titiabdelghafour8-lab/certwatch/internal/config"
	"github.com/titiabdelghafour8-lab/certwatch/internal/metrics"
	"github.com/titiabdelghafour8-lab/certwatch/internal/netguard"
	"github.com/titiabdelghafour8-lab/certwatch/internal/notify"
	"github.com/titiabdelghafour8-lab/certwatch/internal/store"
	"github.com/titiabdelghafour8-lab/certwatch/internal/version"
	"github.com/titiabdelghafour8-lab/certwatch/internal/worker"
)

const usage = `usage: certwatch-server [command]

commands:
  all          run the API and a worker (default)
  serve        run only the HTTP API
  worker       run only the check worker
  migrate      apply database migrations and exit
  healthcheck  probe the local /healthz endpoint (for container health checks)
  version      print version information
`

func main() {
	mode := "all"
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}
	switch mode {
	case "version", "--version", "-v":
		fmt.Println("certwatch-server", version.String())
		return
	case "help", "--help", "-h":
		fmt.Print(usage)
		return
	case "healthcheck":
		os.Exit(healthcheck())
	case "all", "serve", "worker", "migrate":
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", mode, usage)
		os.Exit(2)
	}

	if err := run(mode); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func newLogger(cfg config.Config) *slog.Logger {
	var level slog.Level
	if err := level.UnmarshalText([]byte(cfg.LogLevel)); err != nil {
		level = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: level}
	var h slog.Handler = slog.NewJSONHandler(os.Stdout, opts)
	if cfg.LogFormat == "text" {
		h = slog.NewTextHandler(os.Stdout, opts)
	}
	return slog.New(h)
}

func run(mode string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("configuration: %w", err)
	}
	log := newLogger(cfg)
	slog.SetDefault(log)
	log.Info("starting certwatch-server", "mode", mode, "version", version.Version, "commit", version.Commit)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, cfg.DatabaseURL, log)
	if err != nil {
		return err
	}
	defer st.Close()

	if mode == "migrate" || cfg.AutoMigrate {
		if err := st.Migrate(ctx); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
		if mode == "migrate" {
			log.Info("migrations complete")
			return nil
		}
	}

	reg := metrics.New()
	outbound := netguard.HTTPClient(20*time.Second, cfg.AllowPrivateTargets)
	tlsChecker := &checker.TLSChecker{Timeout: 15 * time.Second, AllowPrivate: cfg.AllowPrivateTargets}
	domainChecker := &checker.DomainChecker{HTTP: netguard.HTTPClient(20*time.Second, false), BootstrapURL: cfg.RDAPBootstrapURL}
	dispatcher := &notify.Dispatcher{HTTP: outbound, SMTP: cfg.SMTP}
	w := worker.New(st, tlsChecker, domainChecker, dispatcher, cfg.Worker, log.With("component", "worker"), reg)

	if !cfg.SMTP.Enabled() {
		log.Warn("SMTP is not configured: signup returns API keys directly and email alerts are disabled")
	}
	if !cfg.Stripe.Enabled() {
		log.Info("Stripe is not configured: billing endpoints are disabled")
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2)

	if mode == "all" || mode == "worker" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- w.Run(ctx)
		}()
	}
	var handler http.Handler
	if mode == "all" || mode == "serve" {
		handler = api.New(cfg, st, w, dispatcher, billing.NewStripe(cfg.Stripe, cfg.PublicURL), log.With("component", "api"), reg).Handler()
	} else {
		// Worker-only processes still expose health and metrics endpoints.
		ops := http.NewServeMux()
		ops.HandleFunc("GET /healthz", func(rw http.ResponseWriter, _ *http.Request) { _, _ = rw.Write([]byte("ok")) })
		ops.Handle("GET /metrics", reg.Handler(cfg.MetricsToken))
		handler = ops
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		errs <- serveHTTP(ctx, cfg.ListenAddr, handler, log)
	}()

	// Stop everything as soon as one component fails.
	go func() {
		for err := range errs {
			if err != nil {
				log.Error("component failed", "err", err)
				stop()
			}
		}
	}()
	wg.Wait()
	close(errs)
	log.Info("shutdown complete")
	return nil
}

func serveHTTP(ctx context.Context, addr string, h http.Handler, log *slog.Logger) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      90 * time.Second, // "check now" can take a while
		IdleTimeout:       120 * time.Second,
	}
	errc := make(chan error, 1)
	go func() {
		log.Info("http listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
		close(errc)
	}()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	log.Info("http shutting down")
	return srv.Shutdown(shutdownCtx)
}

func healthcheck() int {
	addr := os.Getenv("CERTWATCH_LISTEN_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/healthz", nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "unhealthy:", resp.Status)
		return 1
	}
	return 0
}
