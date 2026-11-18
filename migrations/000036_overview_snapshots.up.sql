CREATE TABLE IF NOT EXISTS trader_overview_snapshots (
    venue_id UUID NOT NULL REFERENCES venues(id),
    period TEXT NOT NULL CHECK (period IN ('1D','7D','30D','ALL')),
    pnl_metric TEXT NOT NULL CHECK (pnl_metric IN ('pnl','realized_pnl')),
    computed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    payload JSONB NOT NULL,
    PRIMARY KEY (venue_id, period, pnl_metric)
);
