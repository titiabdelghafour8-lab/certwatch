# Configuration

The server is configured only through environment variables. Invalid values
stop it at startup with a clear message.

## Core

| Variable | Default | Description |
| --- | --- | --- |
| `CERTWATCH_DATABASE_URL` | **required** | PostgreSQL URL, e.g. `postgres://user:pass@host:5432/db?sslmode=require` |
| `CERTWATCH_LISTEN_ADDR` | `:8080` | HTTP listen address (all modes; workers expose `/healthz` and `/metrics`) |
| `CERTWATCH_PUBLIC_URL` | `http://localhost:8080` | Public base URL, used in emails and Stripe redirects |
| `CERTWATCH_AUTO_MIGRATE` | `true` | Apply migrations at startup. Safe with many replicas because an advisory lock is used. |
| `CERTWATCH_TRUST_PROXY` | `false` | Use the last `X-Forwarded-For` hop as the client IP. Enable only behind your own proxy. |
| `CERTWATCH_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `CERTWATCH_LOG_FORMAT` | `json` | `json` or `text` |
| `CERTWATCH_METRICS_TOKEN` | *(empty)* | If set, `/metrics` requires `Authorization: Bearer <token>` |

## Accounts and policy

| Variable | Default | Description |
| --- | --- | --- |
| `CERTWATCH_SIGNUP_ENABLED` | `true` | Allow `POST /v1/signup` |
| `CERTWATCH_DEFAULT_PLAN` | `free` | Plan for new accounts: `free`, `pro`, `agency`, `unlimited` |
| `CERTWATCH_ALLOW_PRIVATE_TARGETS` | `false` | Allow monitoring and webhooks to private and loopback addresses. Only for trusted, single-tenant installs. |
| `CERTWATCH_RESTRICT_EMAIL_CHANNELS` | `true` | Email channels may only target the account's own address |

## Worker

| Variable | Default | Description |
| --- | --- | --- |
| `CERTWATCH_WORKER_CONCURRENCY` | `20` | Checks running in parallel per worker |
| `CERTWATCH_WORKER_BATCH_SIZE` | `100` | Monitors claimed per poll |
| `CERTWATCH_WORKER_POLL_INTERVAL` | `15s` | Idle poll interval |
| `CERTWATCH_TLS_CHECK_INTERVAL` | `6h` | TLS check cadence (hourly when ≤ 7 days left) |
| `CERTWATCH_DOMAIN_CHECK_INTERVAL` | `24h` | Domain check cadence |
| `CERTWATCH_ERROR_RETRY_INTERVAL` | `30m` | Retry delay after a failed check |
| `CERTWATCH_FAILURE_ALERT_THRESHOLD` | `3` | Consecutive failures before status becomes `error` and an alert is sent |
| `CERTWATCH_RDAP_BOOTSTRAP_URL` | IANA | Override the RDAP bootstrap registry |

## Email (SMTP)

Email is enabled when both `CERTWATCH_SMTP_HOST` and `CERTWATCH_SMTP_FROM` are
set. Port 465 uses implicit TLS; other ports use STARTTLS when the server
offers it.

| Variable | Default | Description |
| --- | --- | --- |
| `CERTWATCH_SMTP_HOST` | | SMTP server host |
| `CERTWATCH_SMTP_PORT` | `587` | SMTP port |
| `CERTWATCH_SMTP_USERNAME` | | Username (leave empty for no auth) |
| `CERTWATCH_SMTP_PASSWORD` | | Password |
| `CERTWATCH_SMTP_FROM` | | Sender, e.g. `certwatch <alerts@example.com>` |

With email enabled, signup keys are emailed rather than returned, and key
recovery works. Without it, signup returns the key directly, which suits
private installs.

## Billing (Stripe)

Billing is enabled when `CERTWATCH_STRIPE_SECRET_KEY` and at least one price
are set. See [deployment.md](deployment.md#stripe-billing) for setup.

| Variable | Description |
| --- | --- |
| `CERTWATCH_STRIPE_SECRET_KEY` | Secret key (`sk_live_...` / `sk_test_...`) |
| `CERTWATCH_STRIPE_WEBHOOK_SECRET` | Signing secret of the webhook endpoint (`whsec_...`), required with Stripe |
| `CERTWATCH_STRIPE_PRICE_PRO` | Recurring price ID for the `pro` plan |
| `CERTWATCH_STRIPE_PRICE_AGENCY` | Recurring price ID for the `agency` plan |

## CLI

| Variable | Description |
| --- | --- |
| `CERTWATCH_API_URL` | Server URL (overrides the config file) |
| `CERTWATCH_API_KEY` | API key (overrides the config file), handy in CI |
| `NO_COLOR` | Disable colored output |
