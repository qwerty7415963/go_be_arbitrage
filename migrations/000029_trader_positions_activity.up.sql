-- Trader detail: open-position snapshot + durable closed-trade activity for
-- the external-wallet scanner (read-only observability of watched wallets).
-- Distinct from §10.3 `positions` (platform's own execution positions).

CREATE TABLE IF NOT EXISTS trader_positions (
    venue_id          UUID NOT NULL REFERENCES venues(id),
    wallet_address    TEXT NOT NULL CHECK (wallet_address = lower(wallet_address)),
    coin              TEXT NOT NULL,
    side              TEXT NOT NULL CHECK (side IN ('LONG', 'SHORT')),
    size              NUMERIC(30,12) NOT NULL,
    entry_price       NUMERIC(30,12) NULL,
    mark_price        NUMERIC(30,12) NULL,
    position_value    NUMERIC(30,12) NULL,
    unrealized_pnl    NUMERIC(30,12) NULL,
    return_on_equity  NUMERIC(20,12) NULL,
    liquidation_price NUMERIC(30,12) NULL,
    leverage          NUMERIC(20,8)  NULL,
    max_leverage      NUMERIC(20,8)  NULL,
    margin_used       NUMERIC(30,12) NULL,
    as_of             TIMESTAMPTZ NOT NULL,
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (venue_id, wallet_address, coin),
    FOREIGN KEY (venue_id, wallet_address)
        REFERENCES trader_registry (venue_id, wallet_address) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS trader_position_summary (
    venue_id          UUID NOT NULL,
    wallet_address    TEXT NOT NULL CHECK (wallet_address = lower(wallet_address)),
    account_value     NUMERIC(30,12) NULL,
    total_ntl_pos     NUMERIC(30,12) NULL,
    total_margin_used NUMERIC(30,12) NULL,
    as_of             TIMESTAMPTZ NOT NULL,
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (venue_id, wallet_address),
    FOREIGN KEY (venue_id, wallet_address)
        REFERENCES trader_registry (venue_id, wallet_address) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS trader_trades (
    venue_id       UUID NOT NULL,
    wallet_address TEXT NOT NULL CHECK (wallet_address = lower(wallet_address)),
    market         TEXT NOT NULL,
    side           TEXT NOT NULL CHECK (side IN ('LONG', 'SHORT')),
    opened_at      TIMESTAMPTZ NOT NULL,
    closed_at      TIMESTAMPTZ NOT NULL,
    volume         NUMERIC(30,12) NOT NULL,
    pnl            NUMERIC(30,12) NOT NULL,
    fees           NUMERIC(30,12) NOT NULL,
    fills          INT NOT NULL,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (venue_id, wallet_address, market, opened_at, closed_at),
    FOREIGN KEY (venue_id, wallet_address)
        REFERENCES trader_registry (venue_id, wallet_address) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_trader_trades_wallet_closed
    ON trader_trades (venue_id, wallet_address, closed_at DESC);

ALTER TABLE trader_sync_state
    ADD COLUMN IF NOT EXISTS last_positions_sync_at TIMESTAMPTZ NULL;
