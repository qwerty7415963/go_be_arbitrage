CREATE TABLE IF NOT EXISTS trader_bucket_members (
    venue_id UUID NOT NULL REFERENCES venues(id),
    kind TEXT NOT NULL CHECK (kind IN ('pnl', 'account')),
    bucket_id TEXT NOT NULL,
    period TEXT NOT NULL CHECK (period IN ('1D', '7D', '30D', 'ALL')),
    wallet_address TEXT NOT NULL,
    display_name TEXT,
    perp_equity DOUBLE PRECISION NOT NULL DEFAULT 0,
    position_value DOUBLE PRECISION,
    sum_upnl DOUBLE PRECISION,
    leverage DOUBLE PRECISION NOT NULL DEFAULT 0,
    win_rate DOUBLE PRECISION,
    volume_range DOUBLE PRECISION,
    realized_pnl DOUBLE PRECISION,
    bias TEXT,
    cohort TEXT,
    long_count BIGINT NOT NULL DEFAULT 0,
    short_count BIGINT NOT NULL DEFAULT 0,
    refreshed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (venue_id, kind, bucket_id, period, wallet_address)
);

CREATE INDEX IF NOT EXISTS idx_bucket_members_equity
    ON trader_bucket_members (venue_id, kind, bucket_id, period, perp_equity DESC);
CREATE INDEX IF NOT EXISTS idx_bucket_members_winrate
    ON trader_bucket_members (venue_id, kind, bucket_id, period, win_rate DESC NULLS LAST);
CREATE INDEX IF NOT EXISTS idx_bucket_members_volume
    ON trader_bucket_members (venue_id, kind, bucket_id, period, volume_range DESC NULLS LAST);
CREATE INDEX IF NOT EXISTS idx_bucket_members_pnl
    ON trader_bucket_members (venue_id, kind, bucket_id, period, realized_pnl DESC NULLS LAST);
CREATE INDEX IF NOT EXISTS idx_bucket_members_addr
    ON trader_bucket_members (venue_id, kind, bucket_id, period, wallet_address);

CREATE TABLE IF NOT EXISTS trader_bucket_markets (
    venue_id UUID NOT NULL REFERENCES venues(id),
    kind TEXT NOT NULL CHECK (kind IN ('pnl', 'account')),
    bucket_id TEXT NOT NULL,
    period TEXT NOT NULL CHECK (period IN ('1D', '7D', '30D', 'ALL')),
    range_code TEXT NOT NULL,
    market TEXT NOT NULL,
    notional_long DOUBLE PRECISION NOT NULL DEFAULT 0,
    notional_short DOUBLE PRECISION NOT NULL DEFAULT 0,
    traders_long INT NOT NULL DEFAULT 0,
    traders_short INT NOT NULL DEFAULT 0,
    pnl_profit DOUBLE PRECISION NOT NULL DEFAULT 0,
    pnl_loss DOUBLE PRECISION NOT NULL DEFAULT 0,
    sentiment TEXT,
    asset_id UUID,
    asset_category TEXT,
    logo_url TEXT,
    refreshed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (venue_id, kind, bucket_id, period, range_code, market)
);

CREATE INDEX IF NOT EXISTS idx_bucket_markets_notional
    ON trader_bucket_markets (venue_id, kind, bucket_id, period, range_code, (notional_long + notional_short) DESC);
CREATE INDEX IF NOT EXISTS idx_bucket_markets_pnl
    ON trader_bucket_markets (venue_id, kind, bucket_id, period, range_code, (pnl_profit + pnl_loss) DESC);
CREATE INDEX IF NOT EXISTS idx_bucket_markets_traders
    ON trader_bucket_markets (venue_id, kind, bucket_id, period, range_code, (traders_long + traders_short) DESC);

CREATE TABLE IF NOT EXISTS trader_bucket_detail (
    venue_id UUID NOT NULL REFERENCES venues(id),
    kind TEXT NOT NULL CHECK (kind IN ('pnl', 'account')),
    bucket_id TEXT NOT NULL,
    period TEXT NOT NULL CHECK (period IN ('1D', '7D', '30D', 'ALL')),
    range_code TEXT NOT NULL,
    payload JSONB NOT NULL,
    refreshed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (venue_id, kind, bucket_id, period, range_code)
);

CREATE TABLE IF NOT EXISTS trader_bucket_counts (
    venue_id UUID NOT NULL REFERENCES venues(id),
    kind TEXT NOT NULL CHECK (kind IN ('pnl', 'account')),
    bucket_id TEXT NOT NULL,
    period TEXT NOT NULL CHECK (period IN ('1D', '7D', '30D', 'ALL')),
    wallet_count INT NOT NULL DEFAULT 0,
    markets_count INT NOT NULL DEFAULT 0,
    refreshed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (venue_id, kind, bucket_id, period)
);

CREATE TABLE IF NOT EXISTS trader_bucket_projector_state (
    venue_id UUID PRIMARY KEY REFERENCES venues(id),
    last_source_watermark TIMESTAMPTZ NOT NULL DEFAULT '1970-01-01',
    last_refreshed_at TIMESTAMPTZ NOT NULL DEFAULT '1970-01-01',
    refresh_duration_ms INT NOT NULL DEFAULT 0,
    refresh_status TEXT NOT NULL DEFAULT 'pending'
        CHECK (refresh_status IN ('pending', 'running', 'ready', 'error')),
    last_error TEXT
);

