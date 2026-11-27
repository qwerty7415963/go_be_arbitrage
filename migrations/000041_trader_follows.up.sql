-- Per-user wallet follows (independent of groups). Backfilled from distinct
-- group members; notify_enabled seeded from any notify-enabled group.
CREATE TABLE IF NOT EXISTS trader_follows (
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    venue_id        UUID NOT NULL REFERENCES venues(id),
    wallet_address   TEXT NOT NULL CHECK (wallet_address = lower(wallet_address)),
    notify_enabled   BOOLEAN NOT NULL DEFAULT TRUE,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, venue_id, wallet_address)
);

CREATE INDEX IF NOT EXISTS idx_trader_follows_user_created
    ON trader_follows (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_trader_follows_venue_wallet
    ON trader_follows (venue_id, wallet_address);

INSERT INTO trader_follows (user_id, venue_id, wallet_address, notify_enabled, created_at)
SELECT g.user_id, m.venue_id, m.wallet_address,
       COALESCE(BOOL_OR(g.notify_enabled), FALSE),
       MIN(m.added_at)
FROM trader_group_members m
JOIN trader_groups g ON g.id = m.group_id
GROUP BY g.user_id, m.venue_id, m.wallet_address
ON CONFLICT (user_id, venue_id, wallet_address) DO NOTHING;