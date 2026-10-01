-- Trader Scanner v1.1: keep the computed realized PnL next to the display
-- PnL (spec §9: period PnL shown by the configured source, internal
-- realized kept for reconciliation). Display pnl = leaderboard passthrough
-- when fresh, else the computed realized sum.

ALTER TABLE trader_period_metrics
    ADD COLUMN IF NOT EXISTS realized_pnl NUMERIC(30, 12) NULL;
