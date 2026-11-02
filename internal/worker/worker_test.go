package worker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/titiabdelghafour8/certwatch/internal/checker"
	"github.com/titiabdelghafour8/certwatch/internal/config"
	"github.com/titiabdelghafour8/certwatch/internal/metrics"
	"github.com/titiabdelghafour8/certwatch/internal/model"
	"github.com/titiabdelghafour8/certwatch/internal/notify"
	"github.com/titiabdelghafour8/certwatch/internal/store"
)

type fakeStore struct {
	mu       sync.Mutex
	due      []model.Monitor
	alerts   map[string]bool
	fails    map[int64]int
	success  map[int64]store.CheckSuccess
	channels []model.Channel
}

func newFakeStore() *fakeStore {
	return &fakeStore{alerts: map[string]bool{}, fails: map[int64]int{}, success: map[int64]store.CheckSuccess{}}
}

func (f *fakeStore) ClaimDueMonitors(context.Context, int, time.Duration) ([]model.Monitor, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d := f.due
	f.due = nil
	return d, nil
}

func (f *fakeStore) RecordCheckSuccess(_ context.Context, id int64, r store.CheckSuccess) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.success[id] = r
	f.fails[id] = 0
	return nil
}

func (f *fakeStore) RecordCheckFailure(_ context.Context, id int64, _ string, _ time.Time, _ int) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fails[id]++
	return f.fails[id], nil
}

func (f *fakeStore) InsertAlert(_ context.Context, id int64, kind string, th int, exp *time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := fmt.Sprintf("%d|%s|%d|%s", id, kind, th, exp)
	if f.alerts[key] {
		return false, nil
	}
	f.alerts[key] = true
	return true, nil
}

func (f *fakeStore) ListChannels(context.Context, int64) ([]model.Channel, error) {
	return f.channels, nil
}
func (f *fakeStore) GetUserByID(_ context.Context, id int64) (model.User, error) {
	return model.User{ID: id, Email: "owner@example.com"}, nil
}

type fakeChecker struct {
	res checker.Result
	err error
}

func (c fakeChecker) Check(context.Context, string) (checker.Result, error) { return c.res, c.err }

type fakeNotifier struct {
	mu    sync.Mutex
	sent  []notify.Message
	email bool
}

func (n *fakeNotifier) Send(_ context.Context, _ model.Channel, msg notify.Message) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.sent = append(n.sent, msg)
	return nil
}
func (n *fakeNotifier) EmailEnabled() bool { return n.email }

func cfg() config.WorkerConfig {
	return config.WorkerConfig{
		Concurrency: 4, BatchSize: 10, PollInterval: time.Second,
		TLSInterval: 6 * time.Hour, DomainInterval: 24 * time.Hour,
		ErrorRetry: 30 * time.Minute, FailureThreshold: 3,
	}
}

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestExpiryAlertIsSentOncePerThreshold(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	expires := now.Add(5*24*time.Hour + time.Hour)
	st := newFakeStore()
	st.channels = []model.Channel{{ID: 1, Kind: model.ChannelSlack, Target: "https://hooks.slack.com/x"}}
	n := &fakeNotifier{}
	w := New(st, fakeChecker{res: checker.Result{ExpiresAt: expires}}, fakeChecker{}, n, cfg(), discard(), metrics.New())
	w.now = func() time.Time { return now }

	m := model.Monitor{ID: 7, UserID: 1, Kind: model.KindTLS, Target: "example.com:443", WarnDays: []int{30, 14, 7, 3, 1}}
	got := w.Process(context.Background(), m)
	if got.Status != model.StatusCritical || got.DaysLeft == nil || *got.DaysLeft != 5 {
		t.Fatalf("unexpected monitor after check: status=%s days=%v", got.Status, got.DaysLeft)
	}
	if len(n.sent) != 1 || n.sent[0].Event != notify.EventExpiring {
		t.Fatalf("expected one expiring alert, got %+v", n.sent)
	}
	if next := st.success[7].NextCheck; next != now.Add(time.Hour) {
		t.Errorf("critical monitors should be rechecked hourly, next=%v", next)
	}

	w.Process(context.Background(), m)
	if len(n.sent) != 1 {
		t.Fatalf("alert must not repeat for the same threshold, got %d", len(n.sent))
	}
}

func TestNoAlertWhenFarFromExpiry(t *testing.T) {
	st := newFakeStore()
	n := &fakeNotifier{email: true}
	exp := time.Now().Add(200 * 24 * time.Hour)
	w := New(st, fakeChecker{res: checker.Result{ExpiresAt: exp}}, fakeChecker{}, n, cfg(), discard(), metrics.New())
	got := w.Process(context.Background(), model.Monitor{ID: 1, Kind: model.KindTLS, WarnDays: []int{30}})
	if got.Status != model.StatusOK || len(n.sent) != 0 {
		t.Fatalf("status=%s sent=%d", got.Status, len(n.sent))
	}
}

func TestFailureAlertAfterThreshold(t *testing.T) {
	st := newFakeStore()
	n := &fakeNotifier{email: true} // no channels: falls back to account email
	m := metrics.New()
	w := New(st, fakeChecker{err: errors.New("connection refused")}, fakeChecker{}, n, cfg(), discard(), m)
	mon := model.Monitor{ID: 3, UserID: 1, Kind: model.KindTLS, Target: "down.example.com:443"}

	for i := 1; i <= 4; i++ {
		got := w.Process(context.Background(), mon)
		if i < 3 && got.Status == model.StatusError {
			t.Fatalf("status flipped to error after %d failures", i)
		}
	}
	if len(n.sent) != 1 || n.sent[0].Event != notify.EventCheckFailed {
		t.Fatalf("expected exactly one failure alert, got %+v", n.sent)
	}
	if v := m.Get("certwatch_checks_total", "kind", "tls", "result", "error"); v != 4 {
		t.Errorf("error counter = %d, want 4", v)
	}
}

func TestRunOnceProcessesBatch(t *testing.T) {
	st := newFakeStore()
	for i := int64(1); i <= 5; i++ {
		st.due = append(st.due, model.Monitor{ID: i, Kind: model.KindDomain, WarnDays: []int{30}})
	}
	exp := time.Now().Add(100 * 24 * time.Hour)
	w := New(st, fakeChecker{}, fakeChecker{res: checker.Result{ExpiresAt: exp}}, &fakeNotifier{}, cfg(), discard(), metrics.New())
	n, err := w.RunOnce(context.Background())
	if err != nil || n != 5 || len(st.success) != 5 {
		t.Fatalf("n=%d err=%v recorded=%d", n, err, len(st.success))
	}
}
