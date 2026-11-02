# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org).

## [Unreleased]

## [0.1.0] - 2026-11-02

### Added

- CLI: `check` (local, with `--fail-under` exit codes), `add`, `list`, `show`, `rm`, `recheck`, `pause`, `resume`, `channels`, `signup`, `login`, `logout`, `whoami`, `recover`, `keys`, `upgrade`, `billing`; `--json` output everywhere.
- Server: REST API v1, horizontally scalable worker using PostgreSQL `SKIP LOCKED` leases, TLS and RDAP domain checks.
- Alerts via email, Slack, Discord and webhooks, deduplicated per threshold and certificate.
- Plans (free, pro, agency, unlimited) with Stripe Checkout, customer portal and webhooks.
- SSRF protection, per-IP and per-key rate limiting, Prometheus metrics, structured logging.
- Distroless multi-arch container, Docker Compose for development and production (Caddy), GoReleaser, CI, release and edge-image workflows.
