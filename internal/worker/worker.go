// Package worker runs scheduled checks and sends alerts. Any number of worker
// processes can run against the same database; work is partitioned by
// row-level leases (see store.ClaimDueMonitors).
package worker

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/titiabdelghafour8/certwatch/internal/alerting"
	"github.com/titiabdelghafour8/certwatch/internal/checker"
	"github.com/titiabdelghafour8/certwatch/internal/config"
	"github.com/titiabdelghafour8/certwatch/internal/metrics"
	"github.com/titiabdelghafour8/certwatch/internal/model"
	"github.com/titiabdelghafour8/certwatch/internal/notify"
	"github.com/titiabdelghafour8/certwatch/internal/store"
)

// Store is the subset of persistence the worker needs.
type Store interface {
	ClaimDueMonitors(ctx context.Context, limit int, lease time.Duration) ([]model.Monitor, error)
	RecordCheckSuccess(ctx context.Context, id int64, r store.CheckSuccess) error
	RecordCheckFailure(ctx context.Context, id int64, msg string, next time.Time, failureThreshold int) (int, error)
	InsertAlert(ctx context.Context, monitorID int64, kind string, thresholdDays int, expiresAt *time.Time) (bool, error)
	ListChannels(ctx context.Context, userID int64) ([]model.Channel, error)
	GetUserByID(ctx context.Context, id int64) (model.User, error)
}

// Notifier delivers messages.
type Notifier interface {
	Send(ctx context.Context, ch model.Channel, msg notify.Message) error
	EmailEnabled() bool
}

type Worker struct {
	store    Store
	checkers map[model.MonitorKind]checker.Checker
	notifier Notifier
	cfg      config.WorkerConfig
	log      *slog.Logger
	metrics  *metrics.Registry
	now      func() time.Time
}

func New(st Store, tls, domain checker.Checker, n Notifier, cfg config.WorkerConfig, log *slog.Logger, m *metrics.Registry) *Worker {
	m.Describe("certwatch_checks_total", "Checks performed, by kind and result.")
	m.Describe("certwatch_alerts_total", "Alert deliveries, by channel kind and result.")
	return &Worker{
		store:    st,
		checkers: map[model.MonitorKind]checker.Checker{model.KindTLS: tls, model.KindDomain: domain},
		notifier: n,
		cfg:      cfg,
		log:      log,
		metrics:  m,
		now:      time.Now,
	}
}

// lease is how long a claimed monitor is reserved for this worker. It must
// exceed the worst-case duration of one batch.
func (w *Worker) lease() time.Duration { return 10 * time.Minute }

// Run polls for due monitors until ctx is cancelled.
func (w *Worker) Run(ctx context.Context) error {
	w.log.Info("worker started", "concurrency", w.cfg.Concurrency, "batch", w.cfg.BatchSize, "poll", w.cfg.PollInterval)
	ticker := time.NewTicker(w.cfg.PollInterval)
	defer ticker.Stop()
	for {
		n, err := w.RunOnce(ctx)
		if err != nil && ctx.Err() == nil {
			w.log.Error("worker batch failed", "err", err)
		}
		// A full batch means there is a backlog: keep going without waiting.
		if n == w.cfg.BatchSize && err == nil {
			continue
		}
		select {
		case <-ctx.Done():
			w.log.Info("worker stopped")
			return nil
		case <-ticker.C:
		}
	}
}

// RunOnce claims and processes one batch, returning how many were processed.
func (w *Worker) RunOnce(ctx context.Context) (int, error) {
	monitors, err := w.store.ClaimDueMonitors(ctx, w.cfg.BatchSize, w.lease())
	if err != nil {
		return 0, err
	}
	sem := make(chan struct{}, w.cfg.Concurrency)
	var wg sync.WaitGroup
	for _, m := range monitors {
		wg.Add(1)
		sem <- struct{}{}
		go func(m model.Monitor) {
			defer wg.Done()
			defer func() { <-sem }()
			w.Process(ctx, m)
		}(m)
	}
	wg.Wait()
	return len(monitors), nil
}

func (w *Worker) interval(kind model.MonitorKind) time.Duration {
	if kind == model.KindDomain {
		return w.cfg.DomainInterval
	}
	return w.cfg.TLSInterval
}

// Process checks one monitor, persists the result, sends any due alert and
// returns the updated monitor.
func (w *Worker) Process(ctx context.Context, m model.Monitor) model.Monitor {
	log := w.log.With("monitor_id", m.ID, "kind", m.Kind, "target", m.Target)
	c, ok := w.checkers[m.Kind]
	if !ok {
		log.Error("no checker for monitor kind")
		return m
	}

	checkCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	res, checkErr := c.Check(checkCtx, m.Target)
	cancel()
	now := w.now().UTC()
	m.LastCheckedAt = &now

	if checkErr != nil {
		w.metrics.Inc("certwatch_checks_total", "kind", string(m.Kind), "result", "error")
		next := now.Add(w.cfg.ErrorRetry)
		fails, err := w.store.RecordCheckFailure(ctx, m.ID, checkErr.Error(), next, w.cfg.FailureThreshold)
		if err != nil {
			log.Error("record check failure", "err", err)
			return m
		}
		log.Warn("check failed", "err", checkErr, "consecutive_failures", fails)
		m.ConsecutiveFailures, m.LastError, m.NextCheckAt = fails, checkErr.Error(), next
		if fails >= w.cfg.FailureThreshold {
			m.Status = model.StatusError
		}
		if fails == w.cfg.FailureThreshold {
			w.deliver(ctx, m, notify.FailureMessage(m, checkErr))
		}
		return m
	}

	w.metrics.Inc("certwatch_checks_total", "kind", string(m.Kind), "result", "ok")
	daysLeft := model.DaysUntil(res.ExpiresAt, now)
	status := alerting.StatusFor(daysLeft, m.WarnDays)
	next := now.Add(w.interval(m.Kind))
	if daysLeft <= alerting.CriticalDays && w.interval(m.Kind) > time.Hour {
		next = now.Add(time.Hour) // notice renewals quickly when it matters
	}
	note := ""
	if res.VerifyError != "" {
		note = "certificate does not validate: " + res.VerifyError
	}
	if err := w.store.RecordCheckSuccess(ctx, m.ID, store.CheckSuccess{
		Status: status, ExpiresAt: res.ExpiresAt, Issuer: res.Issuer, Note: note, NextCheck: next,
	}); err != nil {
		log.Error("record check result", "err", err)
		return m
	}
	expires := res.ExpiresAt
	m.Status, m.ExpiresAt, m.Issuer, m.LastError, m.NextCheckAt, m.ConsecutiveFailures = status, &expires, res.Issuer, note, next, 0
	m.FillDaysLeft(now)

	if threshold := alerting.DueThreshold(daysLeft, m.WarnDays); threshold >= 0 {
		inserted, err := w.store.InsertAlert(ctx, m.ID, "expiry", threshold, &expires)
		if err != nil {
			log.Error("record alert", "err", err)
		} else if inserted {
			w.deliver(ctx, m, notify.ExpiryMessage(m, daysLeft))
		}
	}
	return m
}

// deliver sends msg to every channel of the monitor's owner. Users without
// channels fall back to their account email when SMTP is configured.
func (w *Worker) deliver(ctx context.Context, m model.Monitor, msg notify.Message) {
	log := w.log.With("monitor_id", m.ID, "event", msg.Event)
	channels, err := w.store.ListChannels(ctx, m.UserID)
	if err != nil {
		log.Error("list channels", "err", err)
		return
	}
	if len(channels) == 0 && w.notifier.EmailEnabled() {
		u, err := w.store.GetUserByID(ctx, m.UserID)
		if err != nil {
			log.Error("load user for fallback email", "err", err)
			return
		}
		channels = []model.Channel{{Kind: model.ChannelEmail, Target: u.Email}}
	}
	if len(channels) == 0 {
		log.Warn("alert has no delivery channel")
		return
	}
	for _, ch := range channels {
		sendCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err := w.notifier.Send(sendCtx, ch, msg)
		cancel()
		if err != nil {
			w.metrics.Inc("certwatch_alerts_total", "channel", string(ch.Kind), "result", "error")
			log.Error("alert delivery failed", "channel_id", ch.ID, "channel_kind", ch.Kind, "err", err)
			continue
		}
		w.metrics.Inc("certwatch_alerts_total", "channel", string(ch.Kind), "result", "ok")
		log.Info("alert delivered", "channel_id", ch.ID, "channel_kind", ch.Kind)
	}
}
