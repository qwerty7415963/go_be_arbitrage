ALTER TABLE trader_trades ADD COLUMN IF NOT EXISTS entry_price NUMERIC(30,12);
ALTER TABLE trader_trades ADD COLUMN IF NOT EXISTS exit_price NUMERIC(30,12);
ALTER TABLE trader_trades ADD COLUMN IF NOT EXISTS size NUMERIC(30,12);
CREATE INDEX IF NOT EXISTS idx_trader_trades_market_closed ON trader_trades (venue_id, market, closed_at DESC);
