-- Trader Scanner v1.1 schema (spec v1.1 D2/D3): venue-scoped registry,
-- sync state, daily aggregates, equity curve inputs, period metric cache,
-- and trader groups. Grain is (venue, wallet) everywhere so later DEX venues
-- add rows without migrations. Runs parallel to the legacy wallet tables
-- (tracked_wallets, wallet_metric_snapshots, user_wallet_groups, ...),
-- which are dropped at cutover.

CREATE TABLE IF NOT EXISTS trader_registry (
    venue_id UUID NOT NULL REFERENCES venues(id),
    wallet_address TEXT NOT NULL CHECK (wallet_address = lower(wallet_address)),
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_trade_at TIMESTAMPTZ NULL,
    discovery_source TEXT NOT NULL DEFAULT 'leaderboard'
        CHECK (discovery_source IN ('leaderboard', 'ws_trade', 'both', 'manual')),
    leaderboard_seen_at TIMESTAMPTZ NULL,
    display_name TEXT NULL,
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (venue_id, wallet_address)
);

CREATE TABLE IF NOT EXISTS trader_sync_state (
    venue_id UUID NOT NULL,
    wallet_address TEXT NOT NULL,
    fills_last_time TIMESTAMPTZ NULL,
    fills_last_tid BIGINT NULL,
    last_fills_sync_at TIMESTAMPTZ NULL,
    last_portfolio_sync_at TIMESTAMPTZ NULL,
    backfill_start_time TIMESTAMPTZ NULL,
    backfill_completed_at TIMESTAMPTZ NULL,
    sync_status TEXT NOT NULL DEFAULT 'pending'
        CHECK (sync_status IN ('pending', 'syncing', 'ready', 'error')),
    retry_count INT NOT NULL DEFAULT 0,
    last_error TEXT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (venue_id, wallet_address),
    FOREIGN KEY (venue_id, wallet_address)
        REFERENCES trader_registry (venue_id, wallet_address) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS trader_daily_stats (
    venue_id UUID NOT NULL,
    wallet_address TEXT NOT NULL,
    stat_date DATE NOT NULL,
    trade_count BIGINT NOT NULL DEFAULT 0,
    win_count BIGINT NOT NULL DEFAULT 0,
    loss_count BIGINT NOT NULL DEFAULT 0,
    breakeven_count BIGINT NOT NULL DEFAULT 0,
    realized_pnl NUMERIC(30, 12) NULL,
    fees NUMERIC(30, 12) NOT NULL DEFAULT 0,
    volume NUMERIC(30, 12) NULL,
    gross_profit NUMERIC(30, 12) NULL,
    gross_loss NUMERIC(30, 12) NULL,
    long_count BIGINT NOT NULL DEFAULT 0,
    long_wins BIGINT NOT NULL DEFAULT 0,
    short_count BIGINT NOT NULL DEFAULT 0,
    short_wins BIGINT NOT NULL DEFAULT 0,
    holding_time_sec_sum NUMERIC(20, 2) NOT NULL DEFAULT 0,
    holding_time_sec_count BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (venue_id, wallet_address, stat_date),
    FOREIGN KEY (venue_id, wallet_address)
        REFERENCES trader_registry (venue_id, wallet_address) ON DELETE CASCADE
);

-- Populated by the portfolio job in V1.1 (drawdown inputs); table ships in V1
-- so period_metrics.max_drawdown_pct has a stable source from day one.
CREATE TABLE IF NOT EXISTS trader_equity_daily (
    venue_id UUID NOT NULL,
    wallet_address TEXT NOT NULL,
    stat_date DATE NOT NULL,
    start_equity NUMERIC(30, 12) NULL,
    end_equity NUMERIC(30, 12) NULL,
    peak_equity NUMERIC(30, 12) NULL,
    net_cash_flow NUMERIC(30, 12) NULL,
    daily_return NUMERIC(20, 12) NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (venue_id, wallet_address, stat_date),
    FOREIGN KEY (venue_id, wallet_address)
        REFERENCES trader_registry (venue_id, wallet_address) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS trader_period_metrics (
    venue_id UUID NOT NULL,
    wallet_address TEXT NOT NULL,
    period TEXT NOT NULL CHECK (period IN ('1D', '7D', '30D', 'ALL')),
    as_of TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    pnl NUMERIC(30, 12) NULL,
    roi NUMERIC(20, 12) NULL,
    win_rate NUMERIC(8, 4) NULL,
    trade_count BIGINT NULL,
    volume NUMERIC(30, 12) NULL,
    gross_profit NUMERIC(30, 12) NULL,
    gross_loss NUMERIC(30, 12) NULL,
    profit_factor NUMERIC(20, 12) NULL,
    avg_trade_pnl NUMERIC(30, 12) NULL,
    long_count BIGINT NULL,
    long_wins BIGINT NULL,
    short_count BIGINT NULL,
    short_wins BIGINT NULL,
    max_drawdown_pct NUMERIC(10, 6) NULL,
    avg_holding_time_sec NUMERIC(20, 2) NULL,
    last_trade_at TIMESTAMPTZ NULL,
    data_status TEXT NOT NULL DEFAULT 'syncing'
        CHECK (data_status IN ('ready', 'syncing', 'stale', 'error')),
    calculation_version INT NOT NULL DEFAULT 1,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (venue_id, wallet_address, period),
    FOREIGN KEY (venue_id, wallet_address)
        REFERENCES trader_registry (venue_id, wallet_address) ON DELETE CASCADE
);

-- Btree indexes for the indexed scanner sorts (venue, period, metric DESC
-- NULLS LAST + address tiebreak). 90D is not a spec period (no index).
CREATE INDEX IF NOT EXISTS idx_trader_metrics_pnl
    ON trader_period_metrics (venue_id, period, pnl DESC NULLS LAST, wallet_address ASC);
CREATE INDEX IF NOT EXISTS idx_trader_metrics_roi
    ON trader_period_metrics (venue_id, period, roi DESC NULLS LAST, wallet_address ASC);
CREATE INDEX IF NOT EXISTS idx_trader_metrics_win_rate
    ON trader_period_metrics (venue_id, period, win_rate DESC NULLS LAST, wallet_address ASC);
CREATE INDEX IF NOT EXISTS idx_trader_metrics_volume
    ON trader_period_metrics (venue_id, period, volume DESC NULLS LAST, wallet_address ASC);
CREATE INDEX IF NOT EXISTS idx_trader_metrics_trade_count
    ON trader_period_metrics (venue_id, period, trade_count DESC NULLS LAST, wallet_address ASC);
CREATE INDEX IF NOT EXISTS idx_trader_metrics_last_trade
    ON trader_period_metrics (venue_id, period, last_trade_at DESC NULLS LAST, wallet_address ASC);

CREATE TABLE IF NOT EXISTS trader_groups (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id),
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    description TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, name)
);

-- Latest leaderboard window aggregates per wallet (spec v1.1 D4: ROI
-- passthrough source). Three numbers per window only — raw payloads and
-- rank history are never stored (spec: discard).
CREATE TABLE IF NOT EXISTS trader_leaderboard_ref (
    venue_id UUID NOT NULL,
    wallet_address TEXT NOT NULL,
    lb_window TEXT NOT NULL CHECK (lb_window IN ('day', 'week', 'month', 'allTime')),
    pnl NUMERIC(30, 12) NULL,
    roi NUMERIC(20, 12) NULL,
    volume NUMERIC(30, 12) NULL,
    account_value NUMERIC(30, 12) NULL,
    fetched_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (venue_id, wallet_address, lb_window),
    FOREIGN KEY (venue_id, wallet_address)
        REFERENCES trader_registry (venue_id, wallet_address) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS trader_group_members (
    group_id UUID NOT NULL REFERENCES trader_groups(id) ON DELETE CASCADE,
    venue_id UUID NOT NULL REFERENCES venues(id),
    wallet_address TEXT NOT NULL CHECK (wallet_address = lower(wallet_address)),
    alias TEXT NOT NULL DEFAULT '',
    note TEXT NOT NULL DEFAULT '',
    added_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (group_id, venue_id, wallet_address)
);
CREATE INDEX IF NOT EXISTS idx_trader_group_members_wallet
    ON trader_group_members (venue_id, wallet_address);
