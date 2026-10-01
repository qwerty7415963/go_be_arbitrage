-- Rollback for 000024_trader_realized_pnl.

ALTER TABLE trader_period_metrics DROP COLUMN IF EXISTS realized_pnl;
