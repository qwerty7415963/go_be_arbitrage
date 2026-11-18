CREATE INDEX IF NOT EXISTS idx_equity_wallet_date ON trader_equity_daily (venue_id, wallet_address, stat_date DESC) INCLUDE (end_equity);
CREATE INDEX IF NOT EXISTS idx_daily_stats_date ON trader_daily_stats (venue_id, stat_date) INCLUDE (wallet_address, realized_pnl);
CREATE INDEX IF NOT EXISTS idx_period_metrics_cover ON trader_period_metrics (venue_id, period) INCLUDE (wallet_address, pnl, realized_pnl, volume, long_count, short_count, data_status);
