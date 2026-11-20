# Contract v3.1 — Market Detail

## 0. Scope
- New feature: Market Detail page showing per-market stats, snapshot KPIs, wallet/trade tables.
- New table: watched_markets (admin config, Option A).
- No breaking changes to existing endpoints.
- Effort not a constraint; full design.

## 1. Data model

### watched_markets
- venue_id UUID NOT NULL REFERENCES venues(id)
- market TEXT NOT NULL
- enabled BOOLEAN NOT NULL DEFAULT TRUE
- sort_order INT NOT NULL DEFAULT 0
- created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
- updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
- PK: (venue_id, market)

### Market data sources
- trader_trades: volume, pnl, win rate, long/short counts, fees, avg size per wallet/market/range
- trader_positions: current open notional/uPnL by side (tracked wallets only, sync 3h)
- funding_rates: market-wide OI, mark price, index price, funding rate, next funding (collector 30s)
- asset_aliases/assets: logo, category
- trader_registry: display names
- trader_period_metrics/trader_equity_daily: cohort, bias for wallet rows
- trader_group_members: group filter (auth required)

## 2. Endpoints

### GET /api/v1/traders/markets/:market
Params: venue (default hyperliquid), from (date, default now-7d), to (date, default now)
Response:
{
  venue, market, as_of,
  header: { mark_price, index_price, funding_rate, next_funding_at, market_oi (from funding_rates), logo_url, category, enabled },
  snapshot: {
    open_notional_long, open_notional_short, long_short_ratio,
    unrealized_pnl_long, unrealized_pnl_short, net_unrealized_pnl,
    traders_long, traders_short, traders_total,
    margin_used_long, margin_used_short, margin_used_total,
    avg_leverage_long, avg_leverage_short,
    market_oi (from funding_rates, 30s),
    synced_at (trader_positions updated_at)
  },
  coverage: { tracked_wallets_with_positions, total_wallets_in_market }
}
Note: snapshot only from tracked wallets (sync 3h). Market OI is market-wide (HL 30s). No flow metrics, no time-series.

### GET /api/v1/traders/markets/:market/wallets
Params: venue, from, to, win_rate_min, trades_min, last_trade_after, groups (comma-separated UUIDs), sort (volume|pnl|trades|win_rate|last_trade), dir (asc|desc), page, limit (default 20)
Response: { data: [WalletRow], meta: {page, total, total_pages, limit, has_more} }
WalletRow: { address, display_name, cohort, bias, volume, trades, win_rate, realized_pnl, long_count, short_count, last_trade_at, position_side, position_notional, position_upnl, position_leverage, margin_used }

### GET /api/v1/traders/markets/:market/trades
Params: venue, from, to, side (LONG|SHORT|all), page, limit (default 20)
Response: { data: [TradeRow], meta: {...} }
TradeRow: { closed_at, wallet_address, display_name, side, size, entry_price, exit_price, volume, pnl, fees, fills }

### Admin CRUD /api/v1/admin/traders/markets
GET: list all watched markets with enabled/sort_order
POST: {venue, market, enabled, sort_order}
PATCH: {enabled, sort_order}
DELETE: {venue, market}

Auth: JWT + admin role.

## 3. Business rules
- Wallet table base: wallets with closed trades in this market within [from, to]. No open-position-only wallets.
- Date range: from < to, max span 365 days. Default from = now-7d, to = now.
- Market not in watched_markets: still return data (200) with enabled=false in header. No 404.
- Group filter: only applies when authenticated (user's groups). Public users see all wallets.
- Win rate: trade-level (pnl > 0 / total trades) per wallet within market+range.
- Market OI: from funding_rates (HL meta, 30s). If no data: null.
- Snapshot: from trader_positions where coin = market. Label "tracked wallets, synced 3h".
- Sort default: volume desc.
- Cache: 5min for wallets/trades, 30s for header (OI/funding).

## 4. FE
- Route: /[locale]/traders/markets/[market]?venue=...&from=...&to=...
- Entry: clickable market rows in bucket detail table (cursor-pointer, keyboard, aria-label).
- Layout: header (market context) → snapshot KPI ribbon → wallets tab (default) / trades tab.
- No flow section, no time-series charts.
- Filters: date range, win_rate_min, trades_min, last_trade_after, groups (multi, auth only).
- UI select style: same as wallet scanner filter sheet.
- Default limit 20.
- Badge: "Market OI (HL · 30s)", "Tracked wallets · synced 3h".

## 5. Tests
BE: TestMarketDetail_Shape, TestMarketDetail_NotWatched, TestMarketDetail_HeaderOI, TestMarketWallets_Filters, TestMarketWallets_Pagination, TestMarketTrades_Shape, TestAdminMarketCRUD, e2e.
FE: unit (header, snapshot ribbon, wallet table filters, clickable market row), e2e (market detail load, filters, pagination, click from bucket).
TESTPLAN §19.33, test-cases.md.

## 6. Verification
gofmt, go vet, go test -short/-integration/-e2e, go build, swag init.
FE: type-check, lint, format, test, build, playwright.
