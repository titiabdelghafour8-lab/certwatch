// Package store is the PostgreSQL persistence layer. All SQL lives here.
package store

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/titiabdelghafour8-lab/certwatch/internal/model"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("already exists")
)

type Store struct {
	pool *pgxpool.Pool
	log  *slog.Logger
}

// Open connects to PostgreSQL and verifies the connection.
func Open(ctx context.Context, url string, log *slog.Logger) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	if cfg.MaxConns < 10 {
		cfg.MaxConns = 10
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect to database: %w", err)
	}
	return &Store{pool: pool, log: log}, nil
}

func (s *Store) Close()                         { s.pool.Close() }
func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

// Exec runs a raw statement. Intended for maintenance tasks and tests.
func (s *Store) Exec(ctx context.Context, sql string, args ...any) error {
	_, err := s.pool.Exec(ctx, sql, args...)
	return err
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// ---- users ----

const userCols = `id, email, plan, COALESCE(stripe_customer_id, ''), COALESCE(stripe_subscription_id, ''), created_at`

func scanUser(row pgx.Row) (model.User, error) {
	var u model.User
	err := row.Scan(&u.ID, &u.Email, &u.Plan, &u.StripeCustomerID, &u.StripeSubscriptionID, &u.CreatedAt)
	return u, notFound(err)
}

func (s *Store) CreateUser(ctx context.Context, email, plan string) (model.User, error) {
	u, err := scanUser(s.pool.QueryRow(ctx,
		`INSERT INTO users (email, plan) VALUES ($1, $2) RETURNING `+userCols, email, plan))
	if isUniqueViolation(err) {
		return u, ErrConflict
	}
	return u, err
}

func (s *Store) GetUserByID(ctx context.Context, id int64) (model.User, error) {
	return scanUser(s.pool.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE id = $1`, id))
}

func (s *Store) GetUserByEmail(ctx context.Context, email string) (model.User, error) {
	return scanUser(s.pool.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE lower(email) = lower($1)`, email))
}

func (s *Store) GetUserByStripeCustomer(ctx context.Context, customerID string) (model.User, error) {
	return scanUser(s.pool.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE stripe_customer_id = $1`, customerID))
}

// SetBilling records the plan and Stripe identifiers for a user.
func (s *Store) SetBilling(ctx context.Context, userID int64, plan, customerID, subscriptionID string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE users SET plan = $2,
		stripe_customer_id = COALESCE(NULLIF($3, ''), stripe_customer_id),
		stripe_subscription_id = NULLIF($4, ''),
		updated_at = now() WHERE id = $1`, userID, plan, customerID, subscriptionID)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// ---- API keys ----

const keyCols = `id, name, prefix, created_at, last_used_at`

func (s *Store) CreateAPIKey(ctx context.Context, userID int64, name, prefix, hash string) (model.APIKey, error) {
	var k model.APIKey
	err := s.pool.QueryRow(ctx, `INSERT INTO api_keys (user_id, name, prefix, hash)
		VALUES ($1, $2, $3, $4) RETURNING `+keyCols, userID, name, prefix, hash).
		Scan(&k.ID, &k.Name, &k.Prefix, &k.CreatedAt, &k.LastUsedAt)
	return k, err
}

func (s *Store) ListAPIKeys(ctx context.Context, userID int64) ([]model.APIKey, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+keyCols+` FROM api_keys
		WHERE user_id = $1 AND revoked_at IS NULL ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.APIKey, error) {
		var k model.APIKey
		err := r.Scan(&k.ID, &k.Name, &k.Prefix, &k.CreatedAt, &k.LastUsedAt)
		return k, err
	})
}

func (s *Store) RevokeAPIKey(ctx context.Context, userID, keyID int64) error {
	tag, err := s.pool.Exec(ctx, `UPDATE api_keys SET revoked_at = now()
		WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL`, keyID, userID)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// Authenticate resolves an API key hash to its user and key ID.
func (s *Store) Authenticate(ctx context.Context, hash string) (model.User, int64, error) {
	var keyID int64
	var u model.User
	err := s.pool.QueryRow(ctx, `SELECT k.id, u.id, u.email, u.plan,
			COALESCE(u.stripe_customer_id, ''), COALESCE(u.stripe_subscription_id, ''), u.created_at
		FROM api_keys k JOIN users u ON u.id = k.user_id
		WHERE k.hash = $1 AND k.revoked_at IS NULL`, hash).
		Scan(&keyID, &u.ID, &u.Email, &u.Plan, &u.StripeCustomerID, &u.StripeSubscriptionID, &u.CreatedAt)
	if err != nil {
		return u, 0, notFound(err)
	}
	// Throttled so a busy key does not write on every request.
	_, err = s.pool.Exec(ctx, `UPDATE api_keys SET last_used_at = now()
		WHERE id = $1 AND (last_used_at IS NULL OR last_used_at < now() - interval '5 minutes')`, keyID)
	return u, keyID, err
}

// ---- monitors ----

const monitorCols = `id, user_id, kind, target, label, warn_days, paused, status, expires_at, issuer,
	last_error, consecutive_failures, last_checked_at, next_check_at, created_at`

func scanMonitor(row pgx.Row) (model.Monitor, error) {
	var m model.Monitor
	var kind, status string
	var warn []int32
	err := row.Scan(&m.ID, &m.UserID, &kind, &m.Target, &m.Label, &warn, &m.Paused, &status, &m.ExpiresAt,
		&m.Issuer, &m.LastError, &m.ConsecutiveFailures, &m.LastCheckedAt, &m.NextCheckAt, &m.CreatedAt)
	if err != nil {
		return m, notFound(err)
	}
	m.Kind, m.Status = model.MonitorKind(kind), model.Status(status)
	m.WarnDays = make([]int, len(warn))
	for i, w := range warn {
		m.WarnDays[i] = int(w)
	}
	return m, nil
}

func collectMonitors(rows pgx.Rows, err error) ([]model.Monitor, error) {
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.Monitor, error) { return scanMonitor(r) })
}

func toInt32s(xs []int) []int32 {
	out := make([]int32, len(xs))
	for i, x := range xs {
		out[i] = int32(x) //nolint:gosec // values are validated to 1..365
	}
	return out
}

func (s *Store) CreateMonitor(ctx context.Context, m model.Monitor) (model.Monitor, error) {
	out, err := scanMonitor(s.pool.QueryRow(ctx, `INSERT INTO monitors (user_id, kind, target, label, warn_days)
		VALUES ($1, $2, $3, $4, $5) RETURNING `+monitorCols,
		m.UserID, string(m.Kind), m.Target, m.Label, toInt32s(m.WarnDays)))
	if isUniqueViolation(err) {
		return out, ErrConflict
	}
	return out, err
}

func (s *Store) ListMonitors(ctx context.Context, userID int64) ([]model.Monitor, error) {
	return collectMonitors(s.pool.Query(ctx, `SELECT `+monitorCols+` FROM monitors
		WHERE user_id = $1 ORDER BY expires_at ASC NULLS LAST, id`, userID))
}

func (s *Store) GetMonitor(ctx context.Context, userID, id int64) (model.Monitor, error) {
	return scanMonitor(s.pool.QueryRow(ctx, `SELECT `+monitorCols+` FROM monitors WHERE id = $1 AND user_id = $2`, id, userID))
}

func (s *Store) DeleteMonitor(ctx context.Context, userID, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM monitors WHERE id = $1 AND user_id = $2`, id, userID)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

func (s *Store) SetMonitorPaused(ctx context.Context, userID, id int64, paused bool) (model.Monitor, error) {
	return scanMonitor(s.pool.QueryRow(ctx, `UPDATE monitors SET paused = $3, next_check_at = now(), updated_at = now()
		WHERE id = $1 AND user_id = $2 RETURNING `+monitorCols, id, userID, paused))
}

func (s *Store) CountMonitors(ctx context.Context, userID int64) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM monitors WHERE user_id = $1`, userID).Scan(&n)
	return n, err
}

// ClaimDueMonitors atomically leases up to limit due monitors by pushing
// their next_check_at forward by lease. SKIP LOCKED lets any number of
// workers poll concurrently without picking the same rows. If a worker dies,
// the lease expires and another worker retries.
func (s *Store) ClaimDueMonitors(ctx context.Context, limit int, lease time.Duration) ([]model.Monitor, error) {
	return collectMonitors(s.pool.Query(ctx, `UPDATE monitors SET next_check_at = now() + $2 * interval '1 second'
		WHERE id IN (
			SELECT id FROM monitors
			WHERE NOT paused AND next_check_at <= now()
			ORDER BY next_check_at
			LIMIT $1
			FOR UPDATE SKIP LOCKED)
		RETURNING `+monitorCols, limit, lease.Seconds()))
}

// CheckSuccess is the outcome of a successful check.
type CheckSuccess struct {
	Status    model.Status
	ExpiresAt time.Time
	Issuer    string
	Note      string // e.g. certificate verification problems
	NextCheck time.Time
}

func (s *Store) RecordCheckSuccess(ctx context.Context, id int64, r CheckSuccess) error {
	_, err := s.pool.Exec(ctx, `UPDATE monitors SET status = $2, expires_at = $3, issuer = $4, last_error = $5,
		consecutive_failures = 0, last_checked_at = now(), next_check_at = $6, updated_at = now() WHERE id = $1`,
		id, string(r.Status), r.ExpiresAt, r.Issuer, truncate(r.Note, 500), r.NextCheck)
	return err
}

// RecordCheckFailure stores a failed check and returns the new consecutive
// failure count. The status flips to "error" once failureThreshold is reached
// so a single network blip does not change what users see.
func (s *Store) RecordCheckFailure(ctx context.Context, id int64, msg string, next time.Time, failureThreshold int) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `UPDATE monitors SET
			consecutive_failures = consecutive_failures + 1,
			status = CASE WHEN consecutive_failures + 1 >= $4 THEN 'error' ELSE status END,
			last_error = $2, last_checked_at = now(), next_check_at = $3, updated_at = now()
		WHERE id = $1 RETURNING consecutive_failures`, id, truncate(msg, 500), next, failureThreshold).Scan(&n)
	return n, err
}

// InsertAlert records an alert. It returns false if an identical expiry alert
// was already recorded, which makes alert delivery idempotent across workers.
func (s *Store) InsertAlert(ctx context.Context, monitorID int64, kind string, thresholdDays int, expiresAt *time.Time) (bool, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `INSERT INTO alerts (monitor_id, kind, threshold_days, expires_at)
		VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING RETURNING id`, monitorID, kind, thresholdDays, expiresAt).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// ---- channels ----

const channelCols = `id, user_id, kind, target, name, created_at`

func scanChannel(row pgx.Row) (model.Channel, error) {
	var c model.Channel
	var kind string
	err := row.Scan(&c.ID, &c.UserID, &kind, &c.Target, &c.Name, &c.CreatedAt)
	c.Kind = model.ChannelKind(kind)
	return c, notFound(err)
}

func (s *Store) CreateChannel(ctx context.Context, c model.Channel) (model.Channel, error) {
	return scanChannel(s.pool.QueryRow(ctx, `INSERT INTO channels (user_id, kind, target, name)
		VALUES ($1, $2, $3, $4) RETURNING `+channelCols, c.UserID, string(c.Kind), c.Target, c.Name))
}

func (s *Store) ListChannels(ctx context.Context, userID int64) ([]model.Channel, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+channelCols+` FROM channels WHERE user_id = $1 ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.Channel, error) { return scanChannel(r) })
}

func (s *Store) GetChannel(ctx context.Context, userID, id int64) (model.Channel, error) {
	return scanChannel(s.pool.QueryRow(ctx, `SELECT `+channelCols+` FROM channels WHERE id = $1 AND user_id = $2`, id, userID))
}

func (s *Store) DeleteChannel(ctx context.Context, userID, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM channels WHERE id = $1 AND user_id = $2`, id, userID)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

func (s *Store) CountChannels(ctx context.Context, userID int64) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM channels WHERE user_id = $1`, userID).Scan(&n)
	return n, err
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "")
}
