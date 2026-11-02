CREATE TABLE users (
    id                     BIGSERIAL PRIMARY KEY,
    email                  TEXT        NOT NULL,
    plan                   TEXT        NOT NULL DEFAULT 'free',
    stripe_customer_id     TEXT UNIQUE,
    stripe_subscription_id TEXT,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX users_email_lower_idx ON users (lower(email));

CREATE TABLE api_keys (
    id           BIGSERIAL PRIMARY KEY,
    user_id      BIGINT      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name         TEXT        NOT NULL DEFAULT '',
    prefix       TEXT        NOT NULL,
    hash         TEXT        NOT NULL UNIQUE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at TIMESTAMPTZ,
    revoked_at   TIMESTAMPTZ
);
CREATE INDEX api_keys_user_idx ON api_keys (user_id);

CREATE TABLE monitors (
    id                   BIGSERIAL PRIMARY KEY,
    user_id              BIGINT      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind                 TEXT        NOT NULL CHECK (kind IN ('tls', 'domain')),
    target               TEXT        NOT NULL,
    label                TEXT        NOT NULL DEFAULT '',
    warn_days            INTEGER[]   NOT NULL DEFAULT '{30,14,7,3,1}',
    paused               BOOLEAN     NOT NULL DEFAULT false,
    status               TEXT        NOT NULL DEFAULT 'pending',
    expires_at           TIMESTAMPTZ,
    issuer               TEXT        NOT NULL DEFAULT '',
    last_error           TEXT        NOT NULL DEFAULT '',
    consecutive_failures INTEGER     NOT NULL DEFAULT 0,
    last_checked_at      TIMESTAMPTZ,
    next_check_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, kind, target)
);
-- Workers poll this index for due checks.
CREATE INDEX monitors_due_idx ON monitors (next_check_at) WHERE NOT paused;

CREATE TABLE channels (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind       TEXT        NOT NULL CHECK (kind IN ('email', 'slack', 'discord', 'webhook')),
    target     TEXT        NOT NULL,
    name       TEXT        NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX channels_user_idx ON channels (user_id);

CREATE TABLE alerts (
    id             BIGSERIAL PRIMARY KEY,
    monitor_id     BIGINT      NOT NULL REFERENCES monitors (id) ON DELETE CASCADE,
    kind           TEXT        NOT NULL CHECK (kind IN ('expiry', 'error')),
    threshold_days INTEGER     NOT NULL DEFAULT 0,
    expires_at     TIMESTAMPTZ,
    sent_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- One expiry alert per threshold per certificate/registration period. When a
-- certificate is renewed, expires_at changes and the cycle starts over.
CREATE UNIQUE INDEX alerts_expiry_dedupe_idx ON alerts (monitor_id, threshold_days, expires_at) WHERE kind = 'expiry';
