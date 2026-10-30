CREATE TABLE IF NOT EXISTS telegram_bindings (
    user_id      UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    chat_id      BIGINT NOT NULL UNIQUE,
    tg_username  TEXT NULL,
    tg_first_name TEXT NULL,
    locale       TEXT NOT NULL DEFAULT 'vi' CHECK (locale IN ('vi', 'en')),
    status       TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'blocked')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS telegram_link_codes (
    code       TEXT PRIMARY KEY,
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    locale     TEXT NOT NULL DEFAULT 'vi' CHECK (locale IN ('vi', 'en')),
    expires_at TIMESTAMPTZ NOT NULL,
    used_at    TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_telegram_link_codes_user ON telegram_link_codes(user_id);

ALTER TABLE trader_groups
    ADD COLUMN IF NOT EXISTS notify_enabled BOOLEAN NOT NULL DEFAULT FALSE;

CREATE TABLE IF NOT EXISTS telegram_outbox (
    id              BIGSERIAL PRIMARY KEY,
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    chat_id         BIGINT NOT NULL,
    dedup_key       TEXT NULL,
    body            TEXT NOT NULL,
    attempts        INT NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    sent_at         TIMESTAMPTZ NULL,
    last_error      TEXT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_telegram_outbox_dedup
    ON telegram_outbox(dedup_key) WHERE dedup_key IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_telegram_outbox_pending
    ON telegram_outbox(next_attempt_at) WHERE sent_at IS NULL;
