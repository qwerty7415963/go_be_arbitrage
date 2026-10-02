-- Prune support: index last_seen_at for the dead-wallet rule
-- (stale, fully backfilled, no fills, no traded days, no group).

CREATE INDEX IF NOT EXISTS idx_trader_registry_last_seen
    ON trader_registry (last_seen_at);
