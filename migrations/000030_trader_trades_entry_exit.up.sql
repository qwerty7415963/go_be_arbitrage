-- Wallet tabs contract v1 §3: ENTRY/EXIT avg + size for trader_trades.
-- Nullable: recomputed from trader_fill_buffer (~60d); older rows keep NULL.
-- Backfill happens via the next sync pass (ReplaceTradesForDay rewrites the
-- day window); no data migration here.

ALTER TABLE trader_trades ADD COLUMN IF NOT EXISTS entry_price NUMERIC(30,12) NULL;
ALTER TABLE trader_trades ADD COLUMN IF NOT EXISTS exit_price NUMERIC(30,12) NULL;
ALTER TABLE trader_trades ADD COLUMN IF NOT EXISTS size NUMERIC(30,12) NULL;
