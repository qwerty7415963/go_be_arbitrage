-- Rollback for 000027_widen_metrics. May fail if stored values exceed the
-- narrow types (restore path only; forward is the supported direction).

ALTER TABLE trader_period_metrics
    ALTER COLUMN profit_factor TYPE NUMERIC(20, 12);
ALTER TABLE trader_equity_daily
    ALTER COLUMN daily_return TYPE NUMERIC(20, 12);
