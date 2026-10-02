-- Cutover: drop the legacy wallet scanner tables, replaced by the v1.1
-- trader_* tables (migration 000022). Order respects foreign keys.

DROP TABLE IF EXISTS group_wallet_members;
DROP TABLE IF EXISTS user_wallet_tags;
DROP TABLE IF EXISTS user_wallet_watchlist;
DROP TABLE IF EXISTS wallet_fills;
DROP TABLE IF EXISTS wallet_metric_snapshots;
DROP TABLE IF EXISTS user_wallet_groups;
DROP TABLE IF EXISTS tracked_wallets;
