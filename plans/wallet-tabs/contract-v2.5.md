# Contract v2.5 - Precomputed Overview Snapshots

## Table
`trader_overview_snapshots` (venue_id UUID, period TEXT, pnl_metric TEXT, computed_at TIMESTAMPTZ, payload JSONB)
PK: (venue_id, period, pnl_metric).

## Read path
L1 in-memory cache (15min SWR) -> L2 snapshot table (TTL 240s) -> L3 live compute fallback.

## Worker
Refreshes every 120s for all 8 keys (4 period x 2 metric) for hyperliquid venue; runs once on startup.

## API
Shape unchanged (/overview + /overview/series). No auth change, no migration breaking.
