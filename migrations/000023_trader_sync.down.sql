-- Rollback for 000023_trader_sync.

ALTER TABLE trader_period_metrics DROP COLUMN IF EXISTS is_partial;
ALTER TABLE trader_daily_stats DROP COLUMN IF EXISTS last_trade_at;
DROP TABLE IF EXISTS trader_fill_buffer;
