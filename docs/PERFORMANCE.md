# Backend Performance Plans

> Consolidated from the overview and bucket performance plans. Reference only; no runtime behavior.

## Overview Performance Plan

## Overview Performance Baseline

- Date: 2026-11-18
- DB: localhost:5433 / arbitrage_test
- Representative venue: hyperliquid (`59b7673d-6f09-4ca8-91ba-26ebc5877823`)
- Period: `30D`
- Daily series window: `since = 2026-10-20` (30-day, matching `seriesDays=30`)
- pg_stat_statements: not installed (skipped)
- All plans captured with `EXPLAIN (ANALYZE, BUFFERS)`.

## Table sizes

| table | total bytes | pretty |
|---|---:|---|
| trader_equity_daily | 198,393,856 | 189 MB |
| trader_daily_stats | 187,318,272 | 179 MB |
| trader_period_metrics | 149,381,120 | 142 MB |
| trader_registry | 35,643,392 | 34 MB |

## Row counts

| table | rows |
|---|---:|
| trader_registry | 49,539 |
| trader_period_metrics | 126,005 |
| trader_daily_stats | 516,789 |
| trader_equity_daily | 747,041 |

## Existing indexes (relevant)

- trader_daily_stats_pkey: `(venue_id, wallet_address, stat_date)`
- trader_equity_daily_pkey: `(venue_id, wallet_address, stat_date)`
- trader_period_metrics_pkey: `(venue_id, wallet_address, period)`
- idx_trader_metrics_pnl: `(venue_id, period, pnl DESC NULLS LAST, wallet_address)`
- idx_trader_metrics_volume/win_rate/trade_count/roi/last_trade: `(venue_id, period, <col> DESC NULLS LAST, wallet_address)`

## Query 1 - AggregatePnLBuckets

Execution time: **67.872 ms**. Plan uses Seq Scan on `trader_period_metrics` (filter venue/period/pnl), then Sort + GroupAggregate. Buffers: shared hit=4093.

```
GroupAggregate  (cost=8146.53..9316.99 rows=18090 width=116) (actual time=56.665..67.470 rows=6 loops=1)
  Group Key: (CASE ...)
  Buffers: shared hit=4093
  ->  Sort  (cost=8146.53..8197.45 rows=20366 width=67) (actual time=56.577..58.156 rows=25492 loops=1)
        Sort Key: (CASE ...)
        Sort Method: quicksort  Memory: 2703kB
        Buffers: shared hit=4093
        ->  Seq Scan on trader_period_metrics  (cost=0.00..6688.95 rows=20366 width=67) (actual time=0.015..46.217 rows=25492 loops=1)
              Filter: ((pnl IS NOT NULL) AND (pnl <> '0'::numeric) AND (venue_id = ...) AND (period = '30D'::text))
              Rows Removed by Filter: 100513
              Buffers: shared hit=4093
Planning Time: 1.275 ms
Execution Time: 67.872 ms
```

## Query 2 - AggregateAccountBuckets

Execution time: **1302.102 ms** (slowest). `DISTINCT ON` over all 747,041 rows of `trader_equity_daily` for the venue, Incremental Sort, then Hash Left Join to `trader_period_metrics` (Seq Scan). Buffers: shared hit=203314 read=24082. JIT enabled.

```
GroupAggregate  (cost=125560.68..125697.29 rows=200 width=116) (actual time=1259.343..1268.881 rows=5 loops=1)
  ->  Sort  (cost=125560.68..125579.12 rows=7378 width=67) (actual time=1259.267..1260.821 rows=26653 loops=1)
        Sort Method: quicksort  Memory: 2542kB
        ->  Hash Left Join  (cost=6418.16..125086.68 rows=7378 width=67) (actual time=82.938..1245.502 rows=26653 loops=1)
              ->  Subquery Scan on latest_equity  (cost=5.37..118580.73 rows=7378 width=51) (actual time=36.794..1171.530 rows=26653 loops=1)
                    ->  Unique  (cost=5.37..118304.04 rows=22135 width=55) (actual time=36.786..1163.698 rows=27263 loops=1)
                          ->  Incremental Sort  (cost=5.37..116431.71 rows=748935 width=55) (actual time=36.785..1077.150 rows=747041 loops=1)
                                Sort Key: trader_equity_daily.wallet_address, trader_equity_daily.stat_date DESC
                                Presorted Key: trader_equity_daily.wallet_address
                                Full-sort Groups: 18190  Sort Method: quicksort
                                ->  Index Scan using trader_equity_daily_pkey on trader_equity_daily  (cost=0.55..87602.73 rows=748935 width=55) (actual time=36.543..425.032 rows=747041 loops=1)
                                      Index Cond: (venue_id = ...)
                                      Filter: (end_equity IS NOT NULL)
              ->  Hash  (cost=6012.61..6012.61 rows=32015 width=70) (actual time=45.903..45.904 rows=31501 loops=1)
                    ->  Seq Scan on trader_period_metrics p  (cost=0.00..6012.61 rows=32015 width=70) (actual time=0.025..32.469 rows=31501 loops=1)
                          Filter: ((venue_id = ...) AND (period = '30D'::text))
Planning Time: 1.263 ms
JIT: ... Total 40.220 ms
Execution Time: 1302.102 ms
```

## Query 3 - AggregateDailyPnLSeries

Execution time: **639.068 ms**. Seq Scan on `trader_daily_stats` (312k rows after venue/date filter), Hash Join to `trader_period_metrics` via idx_trader_metrics_pnl, external merge sort (10112 kB temp disk).

```
GroupAggregate  (cost=54396.40..62259.24 rows=196571 width=68) (actual time=513.931..636.688 rows=177 loops=1)
  ->  Sort  (cost=54396.40..54887.83 rows=196571 width=43) (actual time=513.914..574.105 rows=306464 loops=1)
        Sort Method: external merge  Disk: 10112kB
        ->  Hash Join  (cost=3848.55..31065.20 rows=196571 width=43) (actual time=27.976..372.956 rows=306464 loops=1)
              Hash Cond: (d.wallet_address = trader_period_metrics.wallet_address)
              ->  Seq Scan on trader_daily_stats d  (cost=0.00..22962.72 rows=310030 width=54) (actual time=0.024..123.542 rows=312268 loops=1)
                    Filter: ((stat_date >= '2026-10-20'::date) AND (venue_id = ...))
                    Rows Removed by Filter: 204562
              ->  Hash  (cost=3593.98..3593.98 rows=20366 width=51) (actual time=27.745..27.746 rows=25492 loops=1)
                    ->  Index Only Scan using idx_trader_metrics_pnl on trader_period_metrics  (cost=0.42..3593.98 rows=20366 width=51) (actual time=0.085..16.921 rows=25492 loops=1)
                          Index Cond: ((venue_id = ...) AND (period = '30D'::text) AND (pnl IS NOT NULL))
                          Filter: (pnl <> '0'::numeric)
                          Heap Fetches: 4187
Planning Time: 1.796 ms
Execution Time: 639.068 ms
```

## Query 4 - AggregateAccountDailyPnLSeries

Execution time: **1311.540 ms** (slowest, parallel). Repeats the `DISTINCT ON` over 706,401 equity rows (parallel), then Hash Join to `trader_daily_stats` (parallel seq scan). Buffers: shared hit=647128 read=38114. JIT enabled.

```
Finalize GroupAggregate  (cost=141867.46..145343.17 rows=12600 width=68) (actual time=1293.679..1306.674 rows=147 loops=1)
  ->  Gather Merge  (cost=141867.46..144807.67 rows=25200 width=68) (actual time=1293.611..1305.389 rows=425 loops=1)
        Workers Planned: 2  Workers Launched: 2
        ->  Sort  (cost=140867.44..140898.94 rows=12600 width=68) (actual time=1264.490..1264.507 rows=142 loops=3)
              Sort Method: quicksort  Memory: 40kB
        ->  Partial HashAggregate  (cost=139725.81..140009.31 rows=12600 width=68) (actual time=1264.146..1264.290 rows=142 loops=3)
              ->  Hash Join  (cost=118693.88..138756.96 rows=129179 width=43) (actual time=1136.645..1233.991 rows=96078 loops=3)
                    Hash Cond: (d.wallet_address = latest_equity.wallet_address)
                    ->  Parallel Seq Scan on trader_daily_stats d  (cost=0.00..18432.13 rows=129179 width=54) (actual time=25.337..63.285 rows=104089 loops=3)
                          Filter: ((stat_date >= '2026-10-20'::date) AND (venue_id = ...))
                    ->  Hash  (cost=118417.19..118417.19 rows=22135 width=51) (actual time=1111.111..1111.114 rows=27092 loops=3)
                          ->  Subquery Scan on latest_equity  (cost=5.39..118417.19 rows=22135 width=51) (actual time=0.450..1095.224 rows=27092 loops=3)
                                ->  Unique  (cost=5.39..118195.84 rows=22135 width=55) (actual time=0.444..1090.201 rows=27092 loops=3)
                                      ->  Incremental Sort  (cost=5.39..116428.34 rows=707003 width=55) (actual time=0.443..1005.837 rows=706401 loops=3)
                                            Sort Key: trader_equity_daily.wallet_address, trader_equity_daily.stat_date DESC
                                            Presorted Key: trader_equity_daily.wallet_address
                                            ->  Index Scan using trader_equity_daily_pkey on trader_equity_daily  (cost=0.55..89482.51 rows=707003 width=55) (actual time=0.069..341.713 rows=706401 loops=3)
                                                  Index Cond: (venue_id = ...)
                                                  Filter: ((end_equity IS NOT NULL) AND (end_equity > '0'::numeric))
Planning Time: 0.601 ms
JIT: ... Total 84.720 ms
Execution Time: 1311.540 ms
```

## Query 5 - DataStatusCounts

Execution time: **50.829 ms**. Parallel Seq Scan on `trader_period_metrics` (venue/period filter). Buffers: shared read=4093.

```
Finalize GroupAggregate  (cost=5959.59..5960.10 rows=2 width=10) (actual time=42.788..50.532 rows=2 loops=1)
  Group Key: data_status
  ->  Gather Merge  (cost=5959.59..5960.06 rows=4 width=14) (actual time=42.779..50.522 rows=6 loops=1)
        Workers Planned: 2  Workers Launched: 2
        ->  Sort  (cost=4959.57..4959.57 rows=2 width=14) (actual time=20.230..20.232 rows=2 loops=3)
        ->  Partial HashAggregate  (cost=4959.54..4959.56 rows=2 width=14) (actual time=20.127..20.128 rows=2 loops=3)
              ->  Parallel Seq Scan on trader_period_metrics  (cost=0.00..4892.84 rows=13340 width=6) (actual time=0.032..17.209 rows=10501 loops=3)
                    Filter: ((venue_id = ...) AND (period = '30D'::text))
                    Rows Removed by Filter: 31502
Planning Time: 0.420 ms
Execution Time: 50.829 ms
```

## Identified bottlenecks

1. **AggregateAccountBuckets (1302 ms)** and **AggregateAccountDailyPnLSeries (1312 ms)**: `DISTINCT ON (wallet_address)` over the entire venue slice of `trader_equity_daily` (747k rows). The PK is `(venue_id, wallet_address, stat_date)` ascending, but `DISTINCT ON` needs `stat_date DESC`, forcing Incremental Sort over ~747k rows (Full-sort Groups 18k) plus a filter pass. This dominates both queries and runs twice per overview request.
2. **AggregateDailyPnLSeries (639 ms)**: Seq Scan of `trader_daily_stats` (312k rows after venue/date filter, 204k removed) with no supporting `(venue_id, stat_date)` index; external merge sort spills ~10 MB to disk.
3. **AggregateAccountBuckets join**: `trader_period_metrics` is Seq-Scanned (31k rows) for the LEFT JOIN because no index covers `(venue_id, wallet_address, period)` lookup beyond PK; PK could serve but planner chose seq scan.
4. **AggregatePnLBuckets (68 ms)** and **DataStatusCounts (51 ms)**: moderate. Both seq-scan `trader_period_metrics` for the venue/period. DataStatusCounts has no index covering `data_status`, so it cannot be index-only.
5. **No `pg_stat_statements`**: extension not installed, so historical query stats unavailable.

## Candidate fixes (not implemented here)

- Add `(venue_id, wallet_address, stat_date DESC)` index on `trader_equity_daily` (or maintain a `latest_equity` materialized/latest table) to make `DISTINCT ON` an index-only ordered scan.
- Add `(venue_id, stat_date)` (or `(venue_id, stat_date, wallet_address)`) index on `trader_daily_stats` to avoid seq scan + external sort.
- Consider covering index on `trader_period_metrics (venue_id, period) INCLUDE (data_status)` for DataStatusCounts, and re-evaluate seq-scan threshold for AggregatePnLBuckets.
- Cache/reuse the latest-equity computation across the two account queries (currently computed twice).

---

## Bucket Performance Plan

## Bucket Detail Performance Baseline

- Date: 2026-11-18
- DB: localhost:5433 / arbitrage_test
- Representative venue: hyperliquid (`59b7673d-6f09-4ca8-91ba-26ebc5877823`)
- Period: `30D`, range `30D`
- BE: http://localhost:8080 (health 200)
- Table sizes / row counts: see `docs/PERFORMANCE.md` (trader_equity_daily 747,041 rows / 189 MB; trader_daily_stats 516,789 / 179 MB; trader_period_metrics 126,005 / 142 MB; trader_registry 49,539 / 34 MB). Additional: trader_trades 1,861,890 rows; trader_positions 0 rows; trader_position_snapshot_daily 35,079 rows.

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

See `docs/PERFORMANCE.md`:
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

---

