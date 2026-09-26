-- Wallet dashboard: wallet identity + user wallet groups + memberships

-- Canonical wallet identity (BR-01: unique per (chain, address))
CREATE TABLE IF NOT EXISTS tracked_wallets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    chain TEXT NOT NULL,
    address TEXT NOT NULL,
    venue_id UUID NULL REFERENCES venues(id),
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (chain, address)
);

-- User-owned wallet groups
CREATE TABLE IF NOT EXISTS user_wallet_groups (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id),
    name TEXT NOT NULL,
    description TEXT NULL,
    color TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, name)
);

CREATE INDEX IF NOT EXISTS idx_groups_owner ON user_wallet_groups (user_id);

-- Group membership (BR-03: unique per (group, wallet); idempotent add)
CREATE TABLE IF NOT EXISTS group_wallet_members (
    group_id UUID NOT NULL REFERENCES user_wallet_groups(id) ON DELETE CASCADE,
    wallet_id UUID NOT NULL REFERENCES tracked_wallets(id),
    added_by UUID NOT NULL REFERENCES users(id),
    added_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (group_id, wallet_id)
);

CREATE INDEX IF NOT EXISTS idx_members_wallet ON group_wallet_members (wallet_id);
