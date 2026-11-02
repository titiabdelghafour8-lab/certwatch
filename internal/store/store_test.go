package store

import (
	"context"
	"io"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/titiabdelghafour8/certwatch/internal/model"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("CERTWATCH_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("CERTWATCH_TEST_DATABASE_URL not set; skipping integration test")
	}
	ctx := context.Background()
	st, err := Open(ctx, dsn, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrations must be idempotent: %v", err)
	}
	if err := st.Exec(ctx, "TRUNCATE users RESTART IDENTITY CASCADE"); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestClaimDueMonitorsIsExclusive(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	u, err := st.CreateUser(ctx, "w@example.com", "unlimited")
	if err != nil {
		t.Fatal(err)
	}
	const total = 50
	for i := 0; i < total; i++ {
		if _, err := st.CreateMonitor(ctx, model.Monitor{UserID: u.ID, Kind: model.KindDomain,
			Target: "d" + string(rune('a'+i%26)) + string(rune('a'+i/26)) + ".com", WarnDays: []int{30}}); err != nil {
			t.Fatal(err)
		}
	}

	// Several workers claim concurrently; every monitor must be claimed exactly once.
	var mu sync.Mutex
	seen := map[int64]int{}
	var wg sync.WaitGroup
	for w := 0; w < 5; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				ms, err := st.ClaimDueMonitors(ctx, 7, time.Minute)
				if err != nil {
					t.Error(err)
					return
				}
				if len(ms) == 0 {
					return
				}
				mu.Lock()
				for _, m := range ms {
					seen[m.ID]++
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if len(seen) != total {
		t.Fatalf("claimed %d distinct monitors, want %d", len(seen), total)
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("monitor %d claimed %d times", id, n)
		}
	}
}

func TestInsertAlertDedupes(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	u, _ := st.CreateUser(ctx, "a@example.com", "free")
	m, err := st.CreateMonitor(ctx, model.Monitor{UserID: u.ID, Kind: model.KindTLS, Target: "example.com:443", WarnDays: []int{7}})
	if err != nil {
		t.Fatal(err)
	}
	exp := time.Now().Add(5 * 24 * time.Hour).Truncate(time.Second)
	first, err := st.InsertAlert(ctx, m.ID, "expiry", 7, &exp)
	if err != nil || !first {
		t.Fatalf("first insert: %v %v", first, err)
	}
	again, err := st.InsertAlert(ctx, m.ID, "expiry", 7, &exp)
	if err != nil || again {
		t.Fatalf("duplicate insert: %v %v", again, err)
	}
	renewed := exp.Add(90 * 24 * time.Hour)
	if ok, _ := st.InsertAlert(ctx, m.ID, "expiry", 7, &renewed); !ok {
		t.Fatal("a renewed certificate must start a new alert cycle")
	}
}

func TestRecordCheckFailureThreshold(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	u, _ := st.CreateUser(ctx, "f@example.com", "free")
	m, _ := st.CreateMonitor(ctx, model.Monitor{UserID: u.ID, Kind: model.KindTLS, Target: "down.example.com:443", WarnDays: []int{7}})
	for i := 1; i <= 3; i++ {
		n, err := st.RecordCheckFailure(ctx, m.ID, "boom", time.Now(), 3)
		if err != nil || n != i {
			t.Fatalf("failure %d: n=%d err=%v", i, n, err)
		}
		got, _ := st.GetMonitor(ctx, u.ID, m.ID)
		wantErr := i >= 3
		if (got.Status == model.StatusError) != wantErr {
			t.Errorf("after %d failures status=%s", i, got.Status)
		}
	}
}
