-- Allow users without email/password (wallet-first auth)
ALTER TABLE users ALTER COLUMN email DROP NOT NULL;
ALTER TABLE users ALTER COLUMN password_hash DROP NOT NULL;

-- Drop old unique constraint on (tenant_id, email) since email can be null
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_tenant_id_email_key;

-- Add auth_method to distinguish registration source
ALTER TABLE users ADD COLUMN IF NOT EXISTS auth_method TEXT NOT NULL DEFAULT 'email'
    CHECK (auth_method IN ('email', 'wallet', 'both'));

-- Wallet addresses: 1 user → many wallets
CREATE TABLE IF NOT EXISTS wallet_addresses (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    address      TEXT NOT NULL,                    -- checksummed (0x...)
    chain_id     BIGINT NOT NULL DEFAULT 1,        -- EVM chain ID
    is_primary   BOOLEAN NOT NULL DEFAULT FALSE,
    verified_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- 1 wallet belongs to 1 user per chain
    UNIQUE(address, chain_id)
);

CREATE INDEX idx_wallet_addresses_user ON wallet_addresses(user_id);
CREATE INDEX idx_wallet_addresses_address ON wallet_addresses(address);
CREATE INDEX idx_wallet_addresses_chain ON wallet_addresses(chain_id);

-- SIWE nonces: short-lived replay protection
CREATE TABLE IF NOT EXISTS wallet_nonces (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    address      TEXT NOT NULL,                    -- checksummed
    chain_id     BIGINT NOT NULL DEFAULT 1,
    nonce        TEXT NOT NULL UNIQUE,             -- 32 chars random hex
    expires_at   TIMESTAMPTZ NOT NULL,             -- TTL 5 min
    used         BOOLEAN NOT NULL DEFAULT FALSE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_wallet_nonces_address ON wallet_nonces(address);
CREATE INDEX idx_wallet_nonces_nonce ON wallet_nonces(nonce);
CREATE INDEX idx_wallet_nonces_expires ON wallet_nonces(expires_at);
