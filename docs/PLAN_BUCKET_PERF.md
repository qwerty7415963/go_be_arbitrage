# Bucket Detail Performance Baseline

- Date: 2026-11-18
- DB: localhost:5433 / arbitrage_test
- Representative venue: hyperliquid (`59b7673d-6f09-4ca8-91ba-26ebc5877823`)
- Period: `30D`, range `30D`
- BE: http://localhost:8080 (health 200)
- Table sizes / row counts: see `docs/PLAN_OVERVIEW_PERF.md` (trader_equity_daily 747,041 rows / 189 MB; trader_daily_stats 516,789 / 179 MB; trader_period_metrics 126,005 / 142 MB; trader_registry 49,539 / 34 MB). Additional: trader_trades 1,861,890 rows; trader_positions 0 rows; trader_position_snapshot_daily 35,079 rows.

## 1. Endpoint timing (3 calls each)

BE caches bucket responses for 5 minutes, so call 1 is cold and calls 2-3 are warm.

| Endpoint | Call 1 (cold) ms | Call 2 (warm) ms | Call 3 (warm) ms | Min ms | Avg ms |
|---|---:|---:|---:|---:|---:|
| `GET /buckets/pnl/profitable` (detail) | 3137 | 24 | 24 | 24 | 1062 |
| `GET /buckets/pnl/profitable/wallets` | 149 | 24 | 23 | 23 | 65 |
| `GET /buckets/pnl/profitable/markets` | 1758 | 25 | 25 | 25 | 603 |

Status code 200 for all calls.

## 2. EXPLAIN (ANALYZE, BUFFERS) - 4 heavy queries

### a. Membership CTE - account bucket (whale), DISTINCT ON equity_daily

Execution Time: **446.656 ms** (client elapsed 457 ms). Plan: Index Only Scan `idx_equity_wallet_date` on `trader_equity_daily` (895,042 index rows, 303,267 heap fetches), Unique (DISTINCT ON), then Subquery Scan filter. Buffers: shared hit=80660 read=12542. Rows returned by membership: 255.

```
Aggregate  (actual time=430.174..430.176 rows=1)
  -> Subquery Scan on latest_equity  (actual time=0.101..430.036 rows=255)
        Filter: end_equity > 0 AND end_equity >= 1e6 AND end_equity < 5e6
        Rows Removed by Filter: 32752
        -> Unique  (actual time=0.089..415.651 rows=33007)
              -> Index Only Scan using idx_equity_wallet_date  (actual time=0.088..302.399 rows=895042)
                    Index Cond: (venue_id = ...)
                    Filter: (end_equity IS NOT NULL)
                    Heap Fetches: 303267
Planning Time: 1.807 ms
Execution Time: 446.656 ms
```

### b. pos CTE - aggregate all trader_positions

Execution Time: **0.035 ms**. Table `trader_positions` is empty (0 rows) in this DB, so the aggregate is trivial. HashAggregate over Seq Scan (0 rows).

```
HashAggregate  (actual time=0.005..0.005 rows=0)
  -> Seq Scan on trader_positions  (actual time=0.004..0.004 rows=0)
Execution Time: 0.035 ms
```

### c. tradevol CTE - aggregate all trader_trades since rangeStart

Execution Time: **452.132 ms** (client elapsed 457.6 ms). Plan: Parallel Seq Scan on `trader_trades` (512,621 rows per worker, 408,920 rows each after filter `closed_at >= rangeStart`), Partial HashAggregate, Gather, Finalize HashAggregate. Disk usage 3,496 kB temp. Buffers: shared read=42987. Rows returned: 30,891 (venue/wallet groups).

```
Finalize HashAggregate  (actual time=395.654..426.801 rows=30891)
  -> Gather (Workers Planned 2, Launched 2)
        -> Parallel Seq Scan on trader_trades  (actual time=0.034..140.183 rows=408286 loops=3)
              Filter: (closed_at >= '2026-10-19')
              Rows Removed by Filter: 211789
Planning Time: 1.610 ms
Execution Time: 452.132 ms
```

### d. BucketMarkets group-by (trades path)

Execution Time: **8.897 ms**. Plan: Index Scan `idx_trader_trades_wallet_closed` on `trader_trades` (1,326 rows), GroupAggregate by market, Sort. Buffers: shared hit=3 read=... Fast because filtered to the 255 bucket wallets via index.

```
Sort  (actual time=0.088..0.089 rows=0? -> 1326 rows)
  -> GroupAggregate  (actual time=0.044..0.045 ... )
        -> Index Scan using idx_trader_trades_wallet_closed  (actual time=0.122..5.234 rows=1326)
Execution Time: 8.897 ms
```

## 3. Table sizes (reference)

See `docs/PLAN_OVERVIEW_PERF.md`:
- trader_equity_daily: 747,041 rows, 189 MB
- trader_daily_stats: 516,789 rows, 179 MB
- trader_period_metrics: 126,005 rows, 142 MB
- trader_registry: 49,539 rows, 34 MB
- trader_trades: 1,861,890 rows
- trader_positions: 0 rows
- trader_position_snapshot_daily: 35,079 rows

## 4. FE waterfall note

The bucket detail page (`arbitrage_fe/src/app/[locale]/(protected)/traders/overview/[kind]/[bucket]/page.tsx`) mounts `useBucketDetail`, and `BucketTables` mounts `useBucketWallets` and `useBucketMarkets` in parallel. All three react-query calls are enabled as soon as `kind`/`bucket` are present, so there is no strict client-side waterfall: detail, wallets and markets are requested concurrently on mount (each cached 60s on the client, 5min on the BE). The detail response additionally carries `markets_source`/`notional_source` and precomputed `markets_top`/`positioning`, so the header/stat bars can render from the detail call alone while the tables populate independently. Cold BE cache (first call of each endpoint) dominates perceived load: detail ~3.1s, markets ~1.8s; warm calls drop to ~24ms.

## Identified bottleneck

The two dominant heavy queries are (a) the account-bucket membership `DISTINCT ON (wallet_address)` over `trader_equity_daily` (446.7 ms) and (c) the `tradevol` aggregate over all `trader_trades` since rangeStart (452.1 ms). Both run on every cold bucket wallets request (tradevol is part of `BucketWallets`); (a) also runs on every bucket detail/markets request via `BucketWalletAddresses`. Together they explain the ~3.1s cold detail time and ~1.8s cold markets time. The `pos` CTE is trivial here because `trader_positions` is empty; markets group-by is fast (8.9 ms) thanks to `idx_trader_trades_wallet_closed`.

## Errors

None. All endpoints returned 200; integration test passed.
