-- Rollback for 000022_trader_scanner: drop the v1.1 scanner tables.
-- Legacy wallet tables are untouched.

DROP TABLE IF EXISTS trader_group_members;
DROP TABLE IF EXISTS trader_groups;
DROP TABLE IF EXISTS trader_leaderboard_ref;
DROP TABLE IF EXISTS trader_period_metrics;
DROP TABLE IF EXISTS trader_equity_daily;
DROP TABLE IF EXISTS trader_daily_stats;
DROP TABLE IF EXISTS trader_sync_state;
DROP TABLE IF EXISTS trader_registry;
