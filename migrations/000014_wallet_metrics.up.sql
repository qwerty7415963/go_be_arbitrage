-- Wallet metric snapshots (scanner read model; all metrics nullable: no
-- data => NULL, never 0 — BR-07)

CREATE TABLE IF NOT EXISTS wallet_metric_snapshots (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    wallet_id UUID NOT NULL REFERENCES tracked_wallets(id) ON DELETE CASCADE,
    venue_id UUID NULL REFERENCES venues(id),
    market TEXT NULL,
    timeframe TEXT NOT NULL,          -- 24H | 7D | 30D | 90D | ALL | custom range key
    realized_pnl NUMERIC(30,12) NULL,
    roi NUMERIC(24,12) NULL,
    win_rate NUMERIC(8,6) NULL,
    volume NUMERIC(30,12) NULL,
    trade_count BIGINT NULL,
    avg_position NUMERIC(30,12) NULL,
    avg_leverage NUMERIC(20,8) NULL,
    long_count BIGINT NULL,
    short_count BIGINT NULL,
    last_active_at TIMESTAMPTZ NULL,
    computed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One row per (wallet, timeframe, venue, market). Postgres treats NULLs as
-- distinct in plain UNIQUE constraints, so COALESCE folds NULL venue/market
-- into constants to keep aggregates unique too.
CREATE UNIQUE INDEX IF NOT EXISTS uq_wallet_metrics_key
    ON wallet_metric_snapshots (
        wallet_id,
        timeframe,
        COALESCE(venue_id, '00000000-0000-0000-0000-000000000000'::uuid),
        COALESCE(market, '')
    );

CREATE INDEX IF NOT EXISTS idx_wallet_metrics_timeframe ON wallet_metric_snapshots (timeframe);
CREATE INDEX IF NOT EXISTS idx_wallet_metrics_wallet ON wallet_metric_snapshots (wallet_id);
