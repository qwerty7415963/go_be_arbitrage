DROP INDEX IF EXISTS idx_tracked_wallets_chain;
DROP INDEX IF EXISTS idx_tracked_wallets_address_trgm;
-- pg_trgm is left installed: other objects may depend on it.
