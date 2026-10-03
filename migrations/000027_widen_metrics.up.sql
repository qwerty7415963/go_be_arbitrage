-- Live finding: account values spanning dust-to-millions overflow the narrow
-- numeric columns (SQLSTATE 22003). Widen to the 30,12 used everywhere else.

ALTER TABLE trader_equity_daily
    ALTER COLUMN daily_return TYPE NUMERIC(30, 12);
ALTER TABLE trader_period_metrics
    ALTER COLUMN profit_factor TYPE NUMERIC(30, 12);
