CREATE TABLE IF NOT EXISTS trader_equity_intraday (
    venue_id UUID NOT NULL REFERENCES venues(id),
    wallet_address TEXT NOT NULL CHECK (wallet_address = lower(wallet_address)),
    sampled_at TIMESTAMPTZ NOT NULL,
    account_value NUMERIC(30,12) NULL,
    PRIMARY KEY (venue_id, wallet_address, sampled_at),
    FOREIGN KEY (venue_id, wallet_address)
        REFERENCES trader_registry (venue_id, wallet_address) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_equity_intraday_wallet
    ON trader_equity_intraday (venue_id, wallet_address, sampled_at DESC);

CREATE TABLE IF NOT EXISTS trader_wallet_event (
    id BIGSERIAL PRIMARY KEY,
    venue_id UUID NOT NULL REFERENCES venues(id),
    wallet_address TEXT NOT NULL CHECK (wallet_address = lower(wallet_address)),
    coin TEXT NOT NULL,
    event_type TEXT NOT NULL CHECK (event_type IN
        ('position_opened','position_increased','position_reduced','position_closed')),
    size_delta NUMERIC(30,12) NOT NULL,
    price NUMERIC(30,12) NULL,
    realized_pnl NUMERIC(30,12) NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    dedup_key TEXT NOT NULL,
    UNIQUE (dedup_key),
    FOREIGN KEY (venue_id, wallet_address)
        REFERENCES trader_registry (venue_id, wallet_address) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_trader_wallet_event_wallet
    ON trader_wallet_event (venue_id, wallet_address, occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_trader_wallet_event_time
    ON trader_wallet_event (occurred_at DESC);
