# Contract v2.6 — Precomputed Read Model for Bucket Detail (P2)

## 0. Scope
- Performance optimization: replaces on-demand aggregation with precomputed read model.
- No API shape change: /buckets/:kind/:id, /buckets/:kind/:id/wallets, /buckets/:kind/:id/markets unchanged.
- No auth change. No breaking change.
- Adds 4 read-model tables + 1 projector state table.

## 1. Tables (migration 000037)

### trader_bucket_members
- venue_id UUID, kind TEXT (pnl/account), bucket_id TEXT, period TEXT, wallet_address TEXT
- display_name TEXT, perp_equity FLOAT8, position_value FLOAT8 (nullable), sum_upnl FLOAT8 (nullable), leverage FLOAT8, win_rate FLOAT8 (nullable), volume_range FLOAT8 (nullable), realized_pnl FLOAT8 (nullable), bias TEXT, cohort TEXT, long_count BIGINT, short_count BIGINT
- refreshed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
- PK: (venue_id, kind, bucket_id, period, wallet_address)
- Indexes: (venue_id, kind, bucket_id, period, perp_equity DESC), (venue_id, kind, bucket_id, period, win_rate DESC NULLS LAST), (venue_id, kind, bucket_id, period, volume_range DESC NULLS LAST), (venue_id, kind, bucket_id, period, realized_pnl DESC NULLS LAST), (venue_id, kind, bucket_id, period, wallet_address)

### trader_bucket_markets
- venue_id UUID, kind TEXT, bucket_id TEXT, period TEXT, range_code TEXT, market TEXT
- notional_long FLOAT8, notional_short FLOAT8, traders_long INT, traders_short INT, pnl_profit FLOAT8, pnl_loss FLOAT8, sentiment TEXT
- asset_id UUID, asset_category TEXT, logo_url TEXT
- refreshed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
- PK: (venue_id, kind, bucket_id, period, range_code, market)
- Indexes: (venue_id, kind, bucket_id, period, range_code, notional_long+notional_short DESC), (venue_id, kind, bucket_id, period, range_code, pnl_profit+pnl_loss DESC), (venue_id, kind, bucket_id, period, range_code, traders_long+traders_short DESC)

### trader_bucket_detail
- venue_id UUID, kind TEXT, bucket_id TEXT, period TEXT, range_code TEXT
- payload JSONB NOT NULL
- refreshed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
- PK: (venue_id, kind, bucket_id, period, range_code)

### trader_bucket_counts
- venue_id UUID, kind TEXT, bucket_id TEXT, period TEXT
- wallet_count INT, markets_count INT
- refreshed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
- PK: (venue_id, kind, bucket_id, period)

### trader_bucket_projector_state
- venue_id UUID PK
- last_source_watermark TIMESTAMPTZ NOT NULL DEFAULT '1970-01-01'
- last_refreshed_at TIMESTAMPTZ NOT NULL DEFAULT '1970-01-01'
- refresh_duration_ms INT NOT NULL DEFAULT 0
- refresh_status TEXT NOT NULL DEFAULT 'pending' CHECK (refresh_status IN ('pending','running','ready','error'))
- last_error TEXT

## 2. Read path
- BucketDetail: SELECT payload FROM trader_bucket_detail WHERE venue_id=$1 AND kind=$2 AND bucket_id=$3 AND period=$4 AND range_code=$5. If not found or stale (> 2x refresh interval): fallback to live compute (existing BucketDetail logic), return result, async trigger refresh.
- BucketWallets: SELECT from trader_bucket_members WHERE venue_id=$1 AND kind=$2 AND bucket_id=$3 AND period=$4 ORDER BY <sort_expr> <dir> NULLS LAST, wallet_address ASC LIMIT $n OFFSET $m. Count from trader_bucket_counts.
- BucketMarkets: SELECT from trader_bucket_markets WHERE venue_id=$1 AND kind=$2 AND bucket_id=$3 AND period=$4 AND range_code=$5 ORDER BY <sort_expr> <dir> LIMIT $n OFFSET $m. Count from trader_bucket_counts.

## 3. Refresh triggers
- Sync-complete hook: after SyncAll finishes, call projector.Refresh(ctx, venueID) if done > 0.
- Startup backfill: goroutine on app start.
- Watermark safety ticker: every 30 min, check max(updated_at) across source tables; rebuild if changed.
- Admin endpoint: POST /api/v1/admin/traders/buckets/refresh (JWT+admin).

## 4. Refresh algorithm
- Full rebuild in one transaction with advisory lock pg_try_advisory_xact_lock(hashtext('trader_bucket_projector:' || venue_id)).
- Steps: delete members/markets/counts for venue → insert from source queries → upsert detail payloads → update projector state.
- If lock not acquired: skip (another instance running).
- Fallback on error: keep old data, log error, set refresh_status='error'.

## 5. Multi-instance
- Advisory lock ensures only one projector runs per venue.
- Readers always see committed data (MVCC).

## 6. Sync interval change
- traderSyncInterval: 6h → 3h (app.go:381).

## 7. Tests
- Integration: projector idempotent (run twice → same data), staleness fallback, advisory lock.
- Unit: read path fallback, sort mapping.
- E2E: bucket detail shape unchanged.
- TESTPLAN §19.31.

## 8. Verification
gofmt, go vet, go test -short/-integration/-e2e, go build, swag init idempotent.
