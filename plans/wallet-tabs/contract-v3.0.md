# Contract v3.0 — Wallet Detail Read Model + WS Channels + RSC

## 0. Scope & compatibility
- Refactor wallet detail from live-first to read-model-first (like Overview v2.5, Bucket v2.6).
- No new DB tables (reuse synced tables: trader_period_metrics, trader_equity_daily, trader_daily_stats, trader_positions, trader_registry).
- No migration. No breaking shape change.
- Backward compat: ?live=1 and ?sync=0 opt-in keep old behavior. Default = read model (synced).
- Old FE clients: if they don't send ?sync, default is synced (new). If they expect live, they must pass ?live=1.

## 1. Endpoints

### GET /api/v1/traders/{wallet}?period=...&sync=1|0
- Default sync=1: reads trader_period_metrics + trader_registry + trader_position_summary from DB.
  - Response: Detail{registry, metrics, period, data_status, as_of, metrics_source: "synced", synced_at}
  - metrics = PeriodMetrics from trader_period_metrics (pnl, realized_pnl, roi, win_rate, trade_count, volume, profit_factor, long/short counts, max_drawdown, last_trade_at, data_status, is_partial, calculation_version)
  - as_of = metrics.as_of (sync time)
  - synced_at = max(period_metrics.updated_at, registry.updated_at)
- sync=0 (?live=1): existing live compute (fetchLivePerformance). Response: metrics_source: "live", as_of = fetch time.
- Auth: public (unchanged).
- Cache: ETag + Cache-Control: public, max-age=15, stale-while-revalidate=45.

### GET /api/v1/traders/{wallet}/performance?venue=...&period=...&mode=...&series_only=...&sync=1|0
- Default sync=1: builds series from trader_daily_stats (realized_pnl) + trader_equity_daily (end_equity) for 24H/7D/30D/ALL ranges.
  - Response: PerformanceDTO with metrics (from period_metrics) + equity + series (4 ranges) + as_of + data_status + metrics_source: "synced"
- sync=0 (?live=1): existing live compute (fetchLivePerformance).
- Auth: public (unchanged).
- Cache: ETag + Cache-Control: public, max-age=15, stale-while-revalidate=45.

### GET /api/v1/traders/{wallet}/positions (unchanged)
- Live HL + cache 60s. WS is primary source.
- No change to shape.

### GET /api/v1/traders/{wallet}/orders|fills|balances|transfers (unchanged)
- Live HL, cache 60s, ETag added.

### GET /api/v1/traders/ws?wallet= (enhanced)
- Backward compatible: default behavior unchanged (all events sent).
- New: client can send {"type":"subscribe","channels":["positions","orders","activity","trades"]} after open.
- If channels specified, server only sends events for those channels.
- New event: wallet.bootstrap.<channel> sent once per subscribe:
  - wallet.bootstrap.positions: PositionSnapshotDTO
  - wallet.bootstrap.orders: []OrderDTO (open orders)
  - wallet.bootstrap.activity: []ActivityFill (recent 200 fills)
  - wallet.bootstrap.trades: ActivityPage (limit 20, sort closed_at desc) — server-computed, cached 60s
- New event: wallet.dropped {channel, seq} on backpressure.
- Heartbeat: ping/pong (existing).
- Auth: public (unchanged).

## 2. BE changes
- Service.Detail: add sync branch (DB read) + live branch (?live=1 or ?sync=0).
- Service.Performance: add sync branch + live branch.
- Handler: parse sync/live params, set ETag/Cache-Control.
- ActivityWSHandler: channel subscription, bootstrap per channel, dropped event.
- WatcherManager: bootstrap channel state, trades bootstrap via Service.Activity (cached).
- No migration.

## 3. FE changes
- RSC shell: page.tsx server component, fetch read model server-side, stream.
- wallet-live.ts: singleton WS manager, channel registry, fan-out, reconnect, backpressure.
- Hooks: useTraderDetail/useTraderPerformance (read model, stale 3m); useTraderPositions/Activity/Orders/Trades (WS channel consumer); useTraderBalances/Fills/Transfers (REST, 60-120s poll active tab only).
- Remove duplicate positions hook (page-level provider).
- Prefetch on hover.
- Badge: Synced X ago / LIVE / RECONNECTING.
- Polling: 0 when WS LIVE, 60s fallback when non-LIVE, 3-5min for read model.

## 4. Freshness
- Statement/performance: 3h (sync), ?live=1 ~60s.
- Positions/activity/orders/trades: real-time via WS.
- Balances/fills/transfers: 60-120s REST.

## 5. Failure
- Read model missing/stale → fallback live compute, badge.
- WS disconnect → REST fallback 60s, badge non-LIVE.
- Bootstrap timeout → per-channel REST fallback.
- Backpressure drop → wallet.dropped, FE refetch channel.
- RSC fetch error → client shell + skeleton + client fetch.

## 6. Tests
BE: TestDetail_SyncedReadModel, TestDetail_LiveOptIn, TestPerformance_SyncedSeries, TestHandler_ETag, TestWS_SubscribeChannels, TestWS_BootstrapPerChannel, TestWS_BackpressureDropped, TestWS_ReconnectRebootstrap, e2e shape.
FE: wallet-live (1 socket, refcount, fan-out, reconnect, visibility), hooks, bootstrap seeds, no duplicate sockets, prefetch, RSC, E2E.
TESTPLAN §19.32, test-cases.md.

## 7. Verification
BE: gofmt, vet, build, unit, integration, e2e, swag idempotent.
FE: type-check, lint, format, test, build, playwright, visual baselines.
