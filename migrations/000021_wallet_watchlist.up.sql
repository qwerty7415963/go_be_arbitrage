-- Per-user wallet watchlist (star toggle; one row per user+wallet).

CREATE TABLE IF NOT EXISTS user_wallet_watchlist (
    user_id UUID NOT NULL REFERENCES users(id),
    wallet_id UUID NOT NULL REFERENCES tracked_wallets(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, wallet_id)
);
