=== CONTRACT v2.1 � Bucket Detail (Strategy C) ===

# Contract v2.1 � Bucket Detail (Strategy C)

Status: Approved by user (2026-11-08). Implements full-fidelity bucket detail page.

## 1. Data Strategy

Phase 1 (this contract):
- Re-enable `trader_trades` persistence (migration 000032: add entry_price/exit_price/size columns + index).
- Re-enable `trader_positions` / `trader_position_summary` persistence (existing tables, no migration).
- Add daily position snapshot table (migration 000033) for positioning chart by notional.
- Markets tab: open positions (latest) primary, closed trades 30D fallback.

Phase 2 (optional, after Phase 1):
- True position-notional daily positioning chart (from daily snapshots).
- Per-wallet market drill-down.

## 2. Migrations

### 000032_bucket_detail_support (up)
```sql
ALTER TABLE trader_trades ADD COLUMN entry_price NUMERIC(30,12);
ALTER TABLE trader_trades ADD COLUMN exit_price NUMERIC(30,12);
ALTER TABLE trader_trades ADD COLUMN size NUMERIC(30,12);
CREATE INDEX idx_trader_trades_market_closed ON trader_trades (venue_id, market, closed_at DESC);
```
Down: drop index, drop columns.

### 000033_position_snapshots_daily (up)
```sql
CREATE TABLE trader_position_snapshot_daily (
    venue_id UUID NOT NULL REFERENCES venues(id),
    wallet_address TEXT NOT NULL CHECK (wallet_address = lower(wallet_address)),
    snapshot_date DATE NOT NULL,
    long_ntl NUMERIC(30,12) NOT NULL DEFAULT 0,
    short_ntl NUMERIC(30,12) NOT NULL DEFAULT 0,
    account_value NUMERIC(30,12) NULL,
    upnl NUMERIC(30,12) NULL,
    as_of TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (venue_id, wallet_address, snapshot_date),
    FOREIGN KEY (venue_id, wallet_address) REFERENCES trader_registry(venue_id, wallet_address) ON DELETE CASCADE
);
CREATE INDEX idx_pos_snap_daily_bucket ON trader_position_snapshot_daily (venue_id, snapshot_date);
```
Down: drop table.

## 3. Sync Changes

- `sync.go recomputeAffected`: after rollup each day, call `ReplaceTradesForDay(ctx, venueID, wallet, day, trades)` (data already in memory).
- Re-enable `ReplacePositions(ctx, venueID, wallet, summary, positions)` for hot tier (6h interval), skip cold tier.
- Daily snapshot: upsert `trader_position_snapshot_daily` for today's date (once per sync pass per wallet, idempotent).
- Prune: `PruneTrades(60d)` + purge `trader_position_snapshot_daily` 30d in retention service.
- `trader_positions`/`trader_position_summary` (latest) already have FK cascade on wallet delete.

## 4. Endpoints

### GET /api/v1/traders/overview/buckets/:kind/:id
Auth: Public.
Path params: kind ? {pnl, account}; id ? bucket list (validate).
Query: venue (default hyperliquid), period (1D/7D/30D/ALL, default 30D), range (24H/7D/30D, default 30D).

Response 200:
{
  "as_of": "RFC3339",
  "kind": "pnl|account",
  "bucket_id": "string",
  "wallet_count": int,
  "sentiment": "string",
  "long_ratio": float64,
  "pnl_split": {"pct_profit": float64, "pct_loss": float64, "usd_profit": float64, "usd_loss": float64},
  "notional_split": {"long_usd": float64, "short_usd": float64},
  "positioning": [{"t": "RFC3339-date", "long_pct": float64}],
  "markets_top": [{"market": "string", "notional": float64, "upnl": float64, "sentiment": "string", "traders": int}],
  "coverage": {"wallets_with_positions": int, "wallets_with_trades": int}
}

Bucket membership predicate (shared with overview):
- pnl kind: trader_period_metrics WHERE venue_id AND period AND bucket_of(pnl_metric) = id AND pnl IS NOT NULL AND pnl <> 0
- account kind: latest trader_equity_daily.end_equity in bucket range (end_equity > 0)

Positioning series: daily long share by trade volume (from trader_trades) over range. If no trades, fall back to trader_daily_stats long_count/short_count share.

Markets top: aggregate trader_positions by market (coin) for wallets in bucket: notional = SUM(|position_value|), upnl = SUM(unrealized_pnl), sentiment from long/short notional ratio, traders = COUNT(DISTINCT wallet). Top 12 by notional desc. If no positions, aggregate trader_trades by market: notional = SUM(volume), upnl = SUM(pnl), sentiment from side volume ratio, traders = COUNT(DISTINCT wallet).

### GET /api/v1/traders/overview/buckets/:kind/:id/wallets
Auth: Public.
Query: venue, period, sort (equity|win_rate|volume|pnl|bias, default equity), dir (asc|desc, default desc), page (default 1), limit (1..100, default 20).

Response 200:
{
  "data": [{"address": "string", "display_name": "string|null", "perp_equity": float64, "win_rate": float64, "volume_range": float64, "realized_pnl": float64, "bias": "string", "cohort": "string"}],
  "meta": {"page": int, "total": int, "total_pages": int, "limit": int, "has_more": bool}
}

Columns:
- PERP EQUITY: latest end_equity (trader_equity_daily) or 0
- WIN RATE: trader_period_metrics.win_rate
- VOLUME (range): SUM(volume) from trader_trades in period range (or trader_period_metrics.volume as fallback)
- REALIZED PNL: trader_period_metrics.realized_pnl
- PERP BIAS: sentiment from long/short ratio
- SIZE COHORT: account bucket id of the wallet

Sort keys: equity, win_rate, volume, pnl, bias. Default equity desc.

### GET /api/v1/traders/overview/buckets/:kind/:id/markets
Auth: Public.
Query: venue, period, range (24H/7D/30D, default 30D), search (optional, ILIKE on market), sort (notional|upnl|traders, default notional), dir (asc|desc, default desc), page (default 1), limit (1..100, default 20).

Response 200:
{
  "data": [{"market": "string", "sentiment": "string", "notional_long": float64, "notional_short": float64, "traders_long": int, "traders_short": int, "pnl_profit": float64, "pnl_loss": float64}],
  "meta": {"page": int, "total": int, "total_pages": int, "limit": int, "has_more": bool}
}

Markets data source (priority):
1. If trader_positions has data for wallets in bucket: aggregate open positions (notional = SUM(|position_value|), upnl = SUM(unrealized_pnl), sentiment from long/short notional ratio, traders = COUNT DISTINCT wallet).
2. Fallback: aggregate trader_trades in range (notional = SUM(volume) by side, upnl = SUM(pnl) split by sign, sentiment from side volume ratio, traders = COUNT DISTINCT wallet).

## 5. Retention
- trader_trades: 60 days (DELETE WHERE closed_at < now() - 60d).
- trader_position_snapshot_daily: 30 days.
- trader_positions/trader_position_summary: cascade on wallet delete (existing FK).
- trader_daily_stats/trader_equity_daily: 400 days (existing).

## 6. Auth
All endpoints public (no JWT), same as /traders/search and /traders/overview.

## 7. Errors
- 400: invalid kind, bucket id, venue, period, range, sort, dir, page, limit, search.
- 404: bucket not found (no wallets match).
- 500: internal.

## 8. Swagger
- Add godoc annotations for all 3 endpoints.
- Run swag init after changes.

## 9. Tests (mandatory)
BE:
- Unit: bucket predicate, sentiment, notional approx, positioning cumulation, market aggregation.
- Handler: validation, defaults, error handling, response shape.
- Integration: seed data, verify aggregates, trades persistence, position snapshots, retention.
- E2E: seed fills ? sync ? trades persist ? detail endpoints return correct data.

FE:
- Unit: components (header, stat bars, chart, treemap, tables), hooks, service.
- E2E: navigation from overview card ? detail page ? header/bars/chart/tabs render; wallets sort; markets search.
- i18n keys in en/vi.
- docs/test-cases.md update.

## 10. No schema changes beyond migrations
- Migration 000032: trades columns + index.
- Migration 000033: position snapshots daily table.
- No new env vars.
- No changes to existing endpoints.
