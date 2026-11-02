# Deployment

This guide takes certwatch from zero to a paid, public service.

## 1. Server (single VM with Docker)

A 1 vCPU / 1 GB VM (Hetzner, DigitalOcean, Lightsail...) comfortably serves
thousands of users.

1. Point a DNS record (e.g. `api.yourdomain.com`) at the VM's IP.
2. Install Docker, then:

   ```sh
   git clone https://github.com/titiabdelghafour8-lab/certwatch && cd certwatch
   cp deploy/.env.example deploy/.env
   $EDITOR deploy/.env          # set CERTWATCH_DOMAIN, PUBLIC_URL, POSTGRES_PASSWORD, SMTP...
   docker compose -f deploy/docker-compose.prod.yml --env-file deploy/.env up -d
   ```

3. Check it: `curl https://api.yourdomain.com/healthz`.

Caddy obtains and renews the HTTPS certificate automatically. The API runs
migrations at startup. Scale workers with `--scale worker=3`.

**Upgrades:** set `CERTWATCH_VERSION=vX.Y.Z` in `deploy/.env` and run
`docker compose ... pull && docker compose ... up -d`.

**Backups:** schedule `pg_dump` from the `postgres` container, or use a
managed PostgreSQL (Neon, Supabase, RDS) by pointing `CERTWATCH_DATABASE_URL`
at it and removing the `postgres` service.

### Other platforms

The image `ghcr.io/titiabdelghafour8-lab/certwatch` runs anywhere containers run:

- **Fly.io / Render / Railway:** deploy the image with command `all` (or two services, `serve` and `worker`), attach a managed PostgreSQL, set the env vars, and set the health check path to `/healthz`.
- **Kubernetes:** a `Deployment` for `serve` (readiness `/readyz`, liveness `/healthz`), another for `worker`, and optionally an init `Job` running `migrate` with `CERTWATCH_AUTO_MIGRATE=false` elsewhere.

## 2. Email

Email is how users get their API key and alerts, so configure it before
launch. Any SMTP provider works (Postmark, Resend, Amazon SES, Mailgun,
Brevo). Verify your sending domain (SPF + DKIM) with the provider, then set
the `CERTWATCH_SMTP_*` variables. Test it by signing up yourself.

## 3. Stripe billing

1. In the Stripe Dashboard create a product **certwatch Pro** with a recurring price (e.g. $9/month), and **certwatch Agency** (e.g. $29/month). Copy the `price_...` IDs into `CERTWATCH_STRIPE_PRICE_PRO` and `CERTWATCH_STRIPE_PRICE_AGENCY`.
2. Under **Developers → API keys**, copy the secret key into `CERTWATCH_STRIPE_SECRET_KEY`.
3. Under **Developers → Webhooks**, add the endpoint `https://api.yourdomain.com/v1/billing/webhook` with these events:
   - `checkout.session.completed`
   - `customer.subscription.updated`
   - `customer.subscription.deleted`

   Copy its signing secret into `CERTWATCH_STRIPE_WEBHOOK_SECRET`.
4. Under **Settings → Billing → Customer portal**, enable the portal and allow plan switching between your two prices and cancellation.
5. Restart: `docker compose ... up -d`.

Test end-to-end with test-mode keys and card `4242 4242 4242 4242` before
switching to live keys. Plan limits live in `internal/plans/plans.go`.

## 4. Point the CLI at your service

Released CLIs default to the URL in the repository variable
`CERTWATCH_DEFAULT_API_URL`. Set it once:

**GitHub → Settings → Secrets and variables → Actions → Variables →**
`CERTWATCH_DEFAULT_API_URL = https://api.yourdomain.com`

Every release built after that works with no flags:
`certwatch signup --email ...`.

## 5. Release

```sh
git tag v1.0.0
git push origin v1.0.0
```

The **Release** workflow publishes:

- CLI binaries for Linux, macOS and Windows (amd64 and arm64) with checksums on GitHub Releases. The install scripts use these.
- The server image `ghcr.io/titiabdelghafour8-lab/certwatch:1.0.0` and `:latest` (multi-arch, with SBOM and provenance).

After the first image push, open **GitHub → Packages → certwatch → Package
settings** and set visibility to **Public** so servers can pull without
credentials.

## 6. Observability

- **Logs:** JSON on stdout with request IDs. Ship them with your platform's log driver.
- **Metrics:** `/metrics` (Prometheus): `certwatch_checks_total{kind,result}`, `certwatch_alerts_total{channel,result}`, `certwatch_http_requests_total{route,code}`. The public Caddy config hides `/metrics`; scrape the containers directly with `CERTWATCH_METRICS_TOKEN`.
- **Suggested alerts:** a rising `certwatch_alerts_total{result="error"}`, a failing `/readyz`, and a stall in `certwatch_checks_total`.

## Launch checklist

- [ ] DNS points to the server and `https://<domain>/healthz` returns `ok`
- [ ] SMTP configured, and signing up yourself delivers the key email
- [ ] Stripe test-mode purchase upgrades the plan, and cancelling returns you to free
- [ ] Switched to live Stripe keys and a live webhook secret
- [ ] `CERTWATCH_DEFAULT_API_URL` repository variable set, then `v1.0.0` tagged
- [ ] GHCR package visibility set to public
- [ ] Database backups scheduled
- [ ] Terms of service and privacy policy published (required by Stripe)
