CREATE TABLE IF NOT EXISTS trader_position_snapshot_daily (
    venue_id UUID NOT NULL REFERENCES venues(id),
    wallet_address TEXT NOT NULL CHECK (wallet_address = lower(wallet_address)),
    snapshot_date DATE NOT NULL,
    long_ntl NUMERIC(30,12) NOT NULL DEFAULT 0,
    short_ntl NUMERIC(30,12) NOT NULL DEFAULT 0,
    account_value NUMERIC(30,12) NULL,
    upnl NUMERIC(30,12) NULL,
    as_of TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (venue_id, wallet_address, snapshot_date),
    FOREIGN KEY (venue_id, wallet_address) REFERENCES trader_registry(venue_id, wallet_address) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_pos_snap_daily_bucket ON trader_position_snapshot_daily (venue_id, snapshot_date);
