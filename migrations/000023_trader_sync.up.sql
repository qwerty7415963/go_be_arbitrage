-- Trader Scanner v1.1 sync support: transient fill staging (purged after
-- rollup — raw fills are never retained permanently, spec non-goal), plus
-- last_trade_at on daily rows and is_partial on period rows.

CREATE TABLE IF NOT EXISTS trader_fill_buffer (
    venue_id UUID NOT NULL,
    wallet_address TEXT NOT NULL,
    market TEXT NOT NULL,
    exchange_tid BIGINT NOT NULL,
    filled_at TIMESTAMPTZ NOT NULL,
    side TEXT NOT NULL CHECK (side IN ('BUY', 'SELL')),
    quantity NUMERIC(30, 12) NOT NULL,
    price NUMERIC(30, 12) NOT NULL,
    closed_pnl NUMERIC(30, 12) NOT NULL DEFAULT 0,
    fee NUMERIC(30, 12) NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (venue_id, wallet_address, market, exchange_tid),
    FOREIGN KEY (venue_id, wallet_address)
        REFERENCES trader_registry (venue_id, wallet_address) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_trader_fill_buffer_wallet_time
    ON trader_fill_buffer (venue_id, wallet_address, filled_at);

ALTER TABLE trader_daily_stats
    ADD COLUMN IF NOT EXISTS last_trade_at TIMESTAMPTZ NULL;

ALTER TABLE trader_period_metrics
    ADD COLUMN IF NOT EXISTS is_partial BOOLEAN NOT NULL DEFAULT FALSE;
