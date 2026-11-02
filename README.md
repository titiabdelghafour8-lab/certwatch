# certwatch

**Never let a TLS certificate or domain expire again.**

[![CI](https://github.com/titiabdelghafour8/certwatch/actions/workflows/ci.yml/badge.svg)](https://github.com/titiabdelghafour8/certwatch/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/titiabdelghafour8/certwatch)](https://github.com/titiabdelghafour8/certwatch/releases)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

certwatch is a command-line tool and service that watches TLS certificates and
domain registrations and alerts you **before** they expire, by email, Slack,
Discord or webhook. It is built for freelancers, agencies and small teams who
look after many sites.

```text
$ certwatch check example.com github.com
TARGET           KIND    EXPIRES     DAYS  STATUS  ISSUER                        NOTE
example.com:443  tls     2026-12-25  53    ok      SSL Corporation
example.com      domain  2027-08-13  283   ok      RESERVED-Internet Assigned …
github.com:443   tls     2027-01-27  86    ok      Sectigo Limited
github.com       domain  2028-10-09  706   ok      MarkMonitor Inc.
```

## Features

- **Instant local checks** with `certwatch check`: no account needed, works on internal hosts, and `--fail-under N` makes it usable in CI and cron.
- **Continuous monitoring** of TLS certificates (any port) and domain registrations (via RDAP, the official successor to WHOIS).
- **Smart alerts** at 30/14/7/3/1 days, configurable per monitor. Each threshold fires once per certificate, and the cycle restarts automatically when the certificate is renewed.
- **Alert channels:** email, Slack, Discord and JSON webhooks.
- **Failure alerts** when a host stays unreachable for several checks in a row.
- **Plans and billing** built in (Stripe Checkout and customer portal), or run it unlimited and self-hosted.
- **Production-grade server:** stateless API, horizontally scalable workers, PostgreSQL, Prometheus metrics, structured logs, SSRF protection, rate limiting and a distroless container.

## Install the CLI

**macOS / Linux**

```sh
curl -fsSL https://raw.githubusercontent.com/titiabdelghafour8/certwatch/main/scripts/install.sh | sh
```

**Windows (PowerShell)**

```powershell
irm https://raw.githubusercontent.com/titiabdelghafour8/certwatch/main/scripts/install.ps1 | iex
```

**With Go**

```sh
go install github.com/titiabdelghafour8/certwatch/cmd/certwatch@latest
```

Prebuilt binaries for every platform are also on the [Releases](https://github.com/titiabdelghafour8/certwatch/releases) page.

## Quick start

```sh
# 1. One-off checks, no account
certwatch check example.com
certwatch check internal.corp:8443 --tls-only --fail-under 14   # exits 1 if < 14 days left

# 2. Continuous monitoring
certwatch signup --email you@example.com      # free plan: 5 monitors
certwatch add example.com shop.example.com    # TLS + domain monitors, checked immediately
certwatch list

# 3. Choose where alerts go (defaults to your account email)
certwatch channels add slack https://hooks.slack.com/services/T000/B000/XXXX
certwatch channels test 1

# 4. Need more?
certwatch upgrade pro
```

Run `certwatch --help` or `certwatch <command> --help` for every option. All
commands accept `--json` for scripting.

| Command | What it does |
| --- | --- |
| `check <host>...` | Check expiry locally right now |
| `add <host>...` | Start monitoring (`--tls-only`, `--domain-only`, `--warn-days 60,30,7`, `--label`) |
| `list`, `show <id>`, `rm <id>` | Inspect and remove monitors |
| `recheck <id>`, `pause <id>`, `resume <id>` | Control individual monitors |
| `channels add/list/rm/test` | Manage alert destinations |
| `signup`, `login`, `logout`, `whoami`, `recover` | Account |
| `keys list/create/revoke` | API keys, e.g. one per CI system |
| `upgrade [plan]`, `billing` | Subscription management |

Configuration is read from flags, then environment variables
(`CERTWATCH_API_URL`, `CERTWATCH_API_KEY`), then the config file saved by
`login`, at `~/.config/certwatch/config.json` or `%AppData%\certwatch\config.json`.

## Run your own server

```sh
git clone https://github.com/titiabdelghafour8/certwatch && cd certwatch
docker compose up --build
certwatch --api-url http://localhost:8080 signup --email you@example.com
```

The development stack includes [Mailpit](http://localhost:8025), where signup
emails and alerts land. For production, follow the [deployment guide](docs/deployment.md).

## Documentation

- [Architecture](docs/architecture.md): how the pieces fit and how it scales
- [Deployment](docs/deployment.md): production setup, Stripe, email, launch checklist
- [Configuration](docs/configuration.md): every environment variable
- [HTTP API](docs/api.md): endpoints and webhook payload format
- [Contributing](CONTRIBUTING.md) · [Security](SECURITY.md) · [Changelog](CHANGELOG.md)

## License

[MIT](LICENSE)
