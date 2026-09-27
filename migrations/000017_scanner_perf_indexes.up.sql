-- Scanner read-path indexes (BE-12 / HARD-04).
-- search is a '%term%' ILIKE over addresses: pg_trgm GIN makes it indexable.
-- chain/dex multi-select filters get a btree each side of their joins.

CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE INDEX IF NOT EXISTS idx_tracked_wallets_address_trgm
    ON tracked_wallets USING gin (address gin_trgm_ops);

CREATE INDEX IF NOT EXISTS idx_tracked_wallets_chain
    ON tracked_wallets (chain);
