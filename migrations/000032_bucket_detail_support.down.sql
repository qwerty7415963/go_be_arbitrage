DROP INDEX IF EXISTS idx_trader_trades_market_closed;
ALTER TABLE trader_trades DROP COLUMN IF EXISTS size;
ALTER TABLE trader_trades DROP COLUMN IF EXISTS exit_price;
ALTER TABLE trader_trades DROP COLUMN IF EXISTS entry_price;
