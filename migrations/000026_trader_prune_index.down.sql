-- Rollback for 000026_trader_prune_index.

DROP INDEX IF EXISTS idx_trader_registry_last_seen;
