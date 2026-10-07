ALTER TABLE trader_sync_state DROP COLUMN IF EXISTS last_positions_sync_at;
DROP INDEX IF EXISTS idx_trader_trades_wallet_closed;
DROP TABLE IF EXISTS trader_trades;
DROP TABLE IF EXISTS trader_position_summary;
DROP TABLE IF EXISTS trader_positions;
