-- Normalized external-wallet fills from venue trader-history feeds
-- (Phase 3: Hyperliquid public userFillsByTime).

CREATE TABLE IF NOT EXISTS wallet_fills (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    wallet_id UUID NOT NULL REFERENCES tracked_wallets(id) ON DELETE CASCADE,
    venue_id UUID NOT NULL REFERENCES venues(id),
    market TEXT NOT NULL,
    exchange_fill_id TEXT NOT NULL,
    position_id TEXT NOT NULL DEFAULT '',  -- logical-position leg (engine groups trades by it)
    filled_at TIMESTAMPTZ NOT NULL,
    side TEXT NOT NULL,                        -- LONG | SHORT
    quantity NUMERIC(30,12) NOT NULL,
    price NUMERIC(30,12) NOT NULL,
    realized_pnl NUMERIC(30,12) NOT NULL DEFAULT 0,  -- net of fee (closedPnl - fee)
    fee NUMERIC(30,12) NOT NULL DEFAULT 0,
    raw JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (venue_id, market, exchange_fill_id)
);

CREATE INDEX IF NOT EXISTS idx_wallet_fills_wallet_time
    ON wallet_fills (wallet_id, filled_at);

-- Partial flag for capped windows (e.g. Hyperliquid only exposes the
-- 10,000 most recent fills per address).
ALTER TABLE wallet_metric_snapshots
    ADD COLUMN IF NOT EXISTS is_partial BOOLEAN NOT NULL DEFAULT FALSE;

INSERT INTO venues (code, name, venue_type, status)
VALUES ('hyperliquid', 'Hyperliquid', 'PERP_DEX', 'ACTIVE')
ON CONFLICT (code) DO NOTHING;
