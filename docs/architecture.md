# Architecture

## Overview

```text
                ┌────────────────┐        HTTPS/JSON         ┌──────────────────────────┐
  you ───────▶  │ certwatch CLI  │ ────────────────────────▶ │  API  (certwatch-server  │
                │ (Go binary)    │                           │        serve)            │
                └───────┬────────┘                           └────────────┬─────────────┘
                        │ `certwatch check` runs                          │
                        │ checks locally, no server                       ▼
                        ▼                                         ┌───────────────┐
                 TLS hosts / RDAP                                 │  PostgreSQL   │
                                                                  └───────▲───────┘
                                                                          │ claim due monitors
                                                                          │ (FOR UPDATE SKIP LOCKED)
                                       ┌──────────────────────────────────┴───────┐
  TLS hosts ◀─── handshake ─────────── │  Workers (certwatch-server worker) × N   │
  RDAP servers ◀─ domain lookup ────── │                                          │ ──▶ email / Slack /
                                       └──────────────────────────────────────────┘     Discord / webhooks
```

There is one server binary with several modes:

| Mode | Runs | Use |
| --- | --- | --- |
| `all` (default) | API + one worker | Small deployments, development |
| `serve` | HTTP API only | Scale API replicas behind a load balancer |
| `worker` | Check scheduler only, plus `/healthz` and `/metrics` | Scale check throughput |
| `migrate` | Applies DB migrations, then exits | Release step / init container |

## Repository layout

```text
cmd/
  certwatch/            CLI entrypoint
  certwatch-server/     server entrypoint (mode switch, wiring, graceful shutdown)
internal/
  model/        domain types + API payloads (single source of the wire format)
  checker/      TLS + RDAP lookups and target normalization (no I/O besides the lookup)
  alerting/     pure decision logic: status and which alert threshold is due
  store/        PostgreSQL repository, embedded SQL migrations
  worker/       scheduler loop: claim → check → persist → alert
  api/          HTTP handlers, auth, rate limiting, Stripe webhook
  notify/       email (SMTP), Slack, Discord, webhook delivery
  billing/      Stripe Checkout, portal, webhook signature verification
  plans/        plan definitions and limits
  netguard/     SSRF-safe dialers and HTTP clients
  ratelimit/    keyed token buckets
  metrics/      dependency-free Prometheus exporter
  config/       environment configuration
  client/       typed Go API client (used by the CLI)
  cli/          cobra commands
  version/      build metadata injected by -ldflags
```

Dependencies point inward: `model`, `alerting` and `checker` know nothing about
HTTP or SQL. The worker depends on small interfaces (`worker.Store`,
`worker.Notifier`), so it is unit-tested with fakes.

## Scheduling and scaling

Each monitor has a `next_check_at`. Workers repeatedly run:

```sql
UPDATE monitors SET next_check_at = now() + <lease>
WHERE id IN (SELECT id FROM monitors
             WHERE NOT paused AND next_check_at <= now()
             ORDER BY next_check_at LIMIT <batch>
             FOR UPDATE SKIP LOCKED)
RETURNING ...
```

- **No coordinator.** Any number of workers can poll at once. `SKIP LOCKED` guarantees that each monitor is claimed by exactly one worker; this is covered by `TestClaimDueMonitorsIsExclusive`.
- **Crash safety.** The claim pushes `next_check_at` forward by a lease (10 minutes). If a worker dies mid-check, the monitor becomes due again when the lease runs out.
- **Cadence.** TLS checks run every 6 hours and domain checks every 24 hours. Monitors within 7 days of expiry are checked hourly, so renewals show up quickly. Failures retry after 30 minutes.
- **Capacity.** One worker running 20 concurrent checks handles tens of thousands of monitors at these cadences. Add workers with `--scale worker=N`.

The API is stateless: authentication is a database lookup by key hash. Run as
many replicas as you need behind a load balancer. Rate-limit buckets are kept
in memory per replica, which is fine for abuse protection.

## Alerting semantics

- `alerting.DueThreshold` returns the most urgent threshold crossed. A monitor added with 5 days left sends one "5 days" alert, not three.
- Alerts are deduplicated in the database by `(monitor, threshold, expires_at)`, so retries and concurrent workers cannot double-send. A renewed certificate has a new `expires_at`, which starts a fresh cycle.
- Check failures only change the status to `error`, and alert, after `CERTWATCH_FAILURE_ALERT_THRESHOLD` failures in a row (default 3). This prevents noise from network blips.
- Users without channels receive alerts at their account email when SMTP is configured.

## Security model

- **API keys** are 190-bit random secrets prefixed with `cw_` (recognizable by secret scanners). Only their SHA-256 hash is stored.
- **Email ownership.** When SMTP is configured, signup emails the key instead of returning it, which proves the user owns the address. `recover` mails a fresh key, and responses never reveal whether an email is registered.
- **SSRF protection.** The hosted service connects to user-supplied hosts and webhook URLs. `netguard` checks the resolved IP right before each connection, which also blocks DNS rebinding, and refuses loopback, private, link-local, CGNAT and similar ranges. Self-hosters can opt out with `CERTWATCH_ALLOW_PRIVATE_TARGETS=true`.
- **Spam prevention.** Email channels may only target the account's own email address, unless `CERTWATCH_RESTRICT_EMAIL_CHANNELS=false`.
- **Tenancy.** Every query is scoped by `user_id`, and integration tests assert cross-tenant access returns 404.
- **Stripe webhooks** are HMAC-verified with replay protection (5-minute tolerance).
- **Container.** distroless, non-root, no shell.

## Extending

| To add... | Touch |
| --- | --- |
| A notification channel (e.g. Telegram, PagerDuty) | `model.ChannelKind`, `notify.Dispatcher.Send`, validation in `api/handlers_channels.go`, a migration updating the `channels.kind` CHECK constraint |
| A monitor kind (e.g. "http keyword") | a `checker.Checker`, `model.MonitorKind`, `worker.New` wiring, migration for the CHECK constraint |
| A plan | `internal/plans`, then a `CERTWATCH_STRIPE_PRICE_<NAME>` variable is picked up automatically for paid plans listed in `plans.Paid` |
| A schema change | a new file `internal/store/migrations/000N_description.sql`. Never edit released migrations. |
