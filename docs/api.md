# HTTP API (v1)

Base URL: your server, e.g. `https://api.example.com`. All requests and
responses are JSON. Authenticated endpoints need:

```http
Authorization: Bearer cw_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
```

Errors always have this shape:

```json
{ "error": { "code": "plan_limit", "message": "the free plan allows 5 monitors; ..." } }
```

| Status | Meaning |
| --- | --- |
| 400 | Validation error (`invalid_json`, `invalid_target`, ...) |
| 401 | Missing, invalid or revoked API key |
| 402 | Plan limit reached (`plan_limit`) |
| 404 | Not found, or belongs to another account |
| 409 | Conflict (`already_exists`, `email_taken`) |
| 429 | Rate limited (see the `Retry-After` header) |
| 501 | Feature not enabled on this server (billing, email) |

Collections are wrapped as `{"items": [...]}` so pagination can be added later
without breaking clients.

## Public endpoints

| Method | Path | Body | Notes |
| --- | --- | --- | --- |
| `GET` | `/healthz` | | Liveness |
| `GET` | `/readyz` | | Readiness (checks the database) |
| `GET` | `/metrics` | | Prometheus; optional bearer token |
| `POST` | `/v1/signup` | `{"email"}` | `201` with `api_key` (no SMTP) or `202` "check your inbox" |
| `POST` | `/v1/recover` | `{"email"}` | Always `202`; emails a new key if the account exists |
| `POST` | `/v1/billing/webhook` | Stripe event | Verified with `Stripe-Signature` |

## Account

| Method | Path | Body |
| --- | --- | --- |
| `GET` | `/v1/me` | Returns user, plan limits, usage and `billing_enabled` |
| `GET` | `/v1/keys` | |
| `POST` | `/v1/keys` | `{"name"}` returns `{"key": {...}, "secret": "cw_..."}` (secret shown once) |
| `DELETE` | `/v1/keys/{id}` | |
| `POST` | `/v1/billing/checkout` | `{"plan": "pro"}` returns `{"url"}` |
| `POST` | `/v1/billing/portal` | Returns `{"url"}` |

## Monitors

| Method | Path | Body |
| --- | --- | --- |
| `GET` | `/v1/monitors` | Sorted by soonest expiry |
| `POST` | `/v1/monitors` | `{"kind": "tls"\|"domain", "target", "label"?, "warn_days"?}` |
| `GET` | `/v1/monitors/{id}` | |
| `DELETE` | `/v1/monitors/{id}` | |
| `POST` | `/v1/monitors/{id}/check` | Runs the check synchronously, returns the updated monitor |
| `POST` | `/v1/monitors/{id}/pause` | |
| `POST` | `/v1/monitors/{id}/resume` | |

Targets are normalized. For `tls`, `https://Example.com/x` becomes
`example.com:443`. For `domain`, `www.shop.example.co.uk` becomes the
registrable domain `example.co.uk`.

Monitor object:

```json
{
  "id": 12,
  "kind": "tls",
  "target": "example.com:443",
  "label": "marketing site",
  "warn_days": [30, 14, 7, 3, 1],
  "paused": false,
  "status": "warning",
  "expires_at": "2026-12-01T23:59:59Z",
  "days_left": 21,
  "issuer": "Let's Encrypt",
  "last_error": "",
  "last_checked_at": "2026-11-10T08:00:00Z",
  "next_check_at": "2026-11-10T14:00:00Z",
  "created_at": "2026-11-01T10:00:00Z"
}
```

`status` is one of `pending`, `ok`, `warning`, `critical` (≤ 7 days),
`expired` or `error`.

## Channels

| Method | Path | Body |
| --- | --- | --- |
| `GET` | `/v1/channels` | Targets are masked |
| `POST` | `/v1/channels` | `{"kind": "email"\|"slack"\|"discord"\|"webhook", "target", "name"?}` |
| `DELETE` | `/v1/channels/{id}` | |
| `POST` | `/v1/channels/{id}/test` | Sends a test notification |

## Webhook payload

`webhook` channels receive a `POST` with `Content-Type: application/json`:

```json
{
  "event": "expiring",
  "subject": "[certwatch] example.com:443 expires in 7 days",
  "text": "🚨 TLS certificate for example.com:443 expires in 7 days (2026-11-17).",
  "monitor": { "id": 12, "kind": "tls", "target": "example.com:443", "...": "..." },
  "days_left": 7,
  "sent_at": "2026-11-10T08:00:03Z"
}
```

`event` is one of `expiring`, `expired`, `check_failed` or `test`. Respond
with any 2xx status. These field names are a stable contract.
