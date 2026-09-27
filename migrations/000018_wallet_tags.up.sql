-- Per-user wallet labels (private tags; one row per user+wallet).

CREATE TABLE IF NOT EXISTS user_wallet_tags (
    user_id UUID NOT NULL REFERENCES users(id),
    wallet_id UUID NOT NULL REFERENCES tracked_wallets(id) ON DELETE CASCADE,
    tag TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, wallet_id)
);
