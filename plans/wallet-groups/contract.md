# Wallet Groups — API & Data Contract v1.1

> **Status:** Approved — v1.1
> **Date:** 2026-11-26
> **Canonical:** `go_be_arbitrage/plans/wallet-groups/contract.md`
> **Mirror:** `FE_arbitrage/plans/wallet-groups/contract.md` (byte-identical)
> **Change control:** any change requires human approval + version bump + mirror sync. Workers are read-only on this file.

## 1. Scope & precedence

Implements Wallet Groups management/analytics on the existing `/groups` UI and `/v1/trader-groups` API. Reuses the shared trader data pipeline; no per-group collectors, no per-group WebSocket subscriptions, no duplicated wallet storage.

Precedence: hard rules > RAO v1 > this contract > repo `AGENTS.md` > task instructions.

Non-goals: email/Discord alerts, cross-group combined portfolio totals, intraday realized PnL, storing raw upstream payloads.

## 2. Existing schema reused (no new model)

| Table | Role | Retention |
|---|---|---|
| `trader_groups` | user-owned group | — |
| `trader_group_members` | many-to-many membership, PK `(group_id, venue_id, wallet_address)` | — |
| `trader_registry` | canonical wallet registry, PK `(venue_id, wallet_address)` | — |
| `trader_sync_state` | per-wallet sync status/timestamps | — |
| `trader_daily_stats` | per-wallet per-day realized PnL, volume, win/loss, gross profit/loss | 400d |
| `trader_equity_daily` | per-wallet daily start/end/peak equity; `net_cash_flow` always NULL | 400d |
| `trader_positions` | current open positions per wallet/coin | live |
| `trader_position_summary` | current account value / total notional per wallet | live |
| `trader_position_snapshot_daily` | daily account value, upnl, long/short notional | 30d → 400d |
| `trader_trades` | closed trades (pnl, volume, fees) | 60d |
| `trader_fill_buffer` | raw fills (BUY/SELL/qty/price/closed_pnl) | 60d |
| `trader_period_metrics` | cached 1D/7D/30D/ALL wallet metrics | live |

Legacy `user_wallet_groups` / `group_wallet_members` (migration 000013) are not used and not modified.

## 3. New schema — migrations 000039 / 000040

```sql
-- 000039_group_features.up.sql
ALTER TABLE trader_groups
    ADD COLUMN IF NOT EXISTS is_favorite BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX IF NOT EXISTS idx_trader_groups_owner_fav
    ON trader_groups (user_id, is_favorite DESC, name ASC);

CREATE TABLE IF NOT EXISTS group_membership_events (
    id BIGSERIAL PRIMARY KEY,
    group_id UUID NOT NULL REFERENCES trader_groups(id) ON DELETE CASCADE,
    venue_id UUID NOT NULL REFERENCES venues(id),
    wallet_address TEXT NOT NULL CHECK (wallet_address = lower(wallet_address)),
    event_type TEXT NOT NULL CHECK (event_type IN ('added','removed')),
    actor_user_id UUID NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_group_membership_events_group
    ON group_membership_events (group_id, created_at DESC);

CREATE TABLE IF NOT EXISTS group_alerts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    group_id UUID NOT NULL REFERENCES trader_groups(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id),
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    type TEXT NOT NULL CHECK (type IN (
        'unrealized_pnl_above','unrealized_pnl_below','realized_pnl_reaches',
        'roi_reaches','position_opened','position_closed','position_increased',
        'position_reduced','exposure_above','long_short_shift',
        'wallet_inactive','wallet_stale','wallet_added','wallet_removed')),
    params JSONB NOT NULL DEFAULT '{}',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    cooldown_seconds INT NOT NULL DEFAULT 300 CHECK (cooldown_seconds BETWEEN 0 AND 86400),
    notify_in_app BOOLEAN NOT NULL DEFAULT TRUE,
    notify_telegram BOOLEAN NOT NULL DEFAULT FALSE,
    last_condition_met BOOLEAN NOT NULL DEFAULT FALSE,
    last_value NUMERIC(30,12) NULL,
    last_triggered_at TIMESTAMPTZ NULL,
    last_evaluated_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (group_id, name)
);
CREATE INDEX IF NOT EXISTS idx_group_alerts_enabled ON group_alerts (enabled) WHERE enabled;

CREATE TABLE IF NOT EXISTS group_alert_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    alert_id UUID NOT NULL REFERENCES group_alerts(id) ON DELETE CASCADE,
    group_id UUID NOT NULL REFERENCES trader_groups(id) ON DELETE CASCADE,
    type TEXT NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}',
    dedup_key TEXT NOT NULL,
    triggered_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    read_at TIMESTAMPTZ NULL,
    UNIQUE (dedup_key)
);
CREATE INDEX IF NOT EXISTS idx_group_alert_events_group
    ON group_alert_events (group_id, triggered_at DESC);
CREATE INDEX IF NOT EXISTS idx_group_alert_events_unread
    ON group_alert_events (group_id, triggered_at DESC) WHERE read_at IS NULL;

-- 000040_history_events.up.sql
CREATE TABLE IF NOT EXISTS trader_equity_intraday (
    venue_id UUID NOT NULL REFERENCES venues(id),
    wallet_address TEXT NOT NULL CHECK (wallet_address = lower(wallet_address)),
    sampled_at TIMESTAMPTZ NOT NULL,
    account_value NUMERIC(30,12) NULL,
    PRIMARY KEY (venue_id, wallet_address, sampled_at),
    FOREIGN KEY (venue_id, wallet_address)
        REFERENCES trader_registry (venue_id, wallet_address) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_equity_intraday_wallet
    ON trader_equity_intraday (venue_id, wallet_address, sampled_at DESC);

CREATE TABLE IF NOT EXISTS trader_wallet_event (
    id BIGSERIAL PRIMARY KEY,
    venue_id UUID NOT NULL REFERENCES venues(id),
    wallet_address TEXT NOT NULL CHECK (wallet_address = lower(wallet_address)),
    coin TEXT NOT NULL,
    event_type TEXT NOT NULL CHECK (event_type IN
        ('position_opened','position_increased','position_reduced','position_closed')),
    size_delta NUMERIC(30,12) NOT NULL,
    price NUMERIC(30,12) NULL,
    realized_pnl NUMERIC(30,12) NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    dedup_key TEXT NOT NULL,
    UNIQUE (dedup_key),
    FOREIGN KEY (venue_id, wallet_address)
        REFERENCES trader_registry (venue_id, wallet_address) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_trader_wallet_event_wallet
    ON trader_wallet_event (venue_id, wallet_address, occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_trader_wallet_event_time
    ON trader_wallet_event (occurred_at DESC);
```

Down migrations drop the new tables/indexes and the column.

Retention changes (config, no schema): `TraderPositionSnapshotDailyMaxAge` 30d → 400d; add `TraderEquityIntradayMaxAge=14d`, `TraderWalletEventMaxAge=90d`, `GroupAlertEventMaxAge=365d`.

## 4. Metric definitions

Let `W_g` = distinct `(venue_id, wallet_address)` members of group `g`. Period `P`:
- `1D` = last 24h; `7D/30D/90D` = last N UTC days including today; `ALL` = earliest stored day → today (≤400d).

| Metric | Definition | Null rule |
|---|---|---|
| `account_value` | `Σ latest summary.account_value`, fallback latest `end_equity` | wallets with data only; `partial=true` if any missing |
| `unrealized_pnl` | `Σ trader_positions.unrealized_pnl` | `null` if no member has position data |
| `total_notional` | `Σ abs(position_value)` (gross) | `null` if no data |
| `realized_pnl` | `Σ trader_daily_stats.realized_pnl` over P | `null` if no rows |
| `volume` | `Σ trader_daily_stats.volume` over P | `null` if no rows |
| `trade_count` | `Σ trade_count` | `null` if no rows |
| `win_count` | `Σ win_count` over P | `0` is valid |
| `loss_count` | `Σ loss_count` over P | `0` is valid |
| `breakeven_count` | `Σ breakeven_count` over P | `0` is valid |
| `win_rate` | `100 · Σ win_count / (Σ win_count + Σ loss_count)` (breakeven excluded) | `null` if denominator 0 |
| `profit_factor` | `Σ gross_profit / abs(Σ gross_loss)` | `null` if `loss_count == 0` or `trade_count < 10` (min sample 10) |
| `r_t` | per-wallet TWR day return `end/start − 1` (wallets with both start/end > 0); member missing from a day excluded from that day's aggregate | skipped day if no wallet has both |
| `roi` | money-weighted/IRR over combined daily equity + ledger cash flows; per-wallet TWR aggregate weighted by start equity; flow-adjusted using `userNonFundingLedgerUpdates` (deposit/withdraw/internal/subaccount/vault/spot/account-class transfers) | `null` if no valid day or ledger/equity unavailable |
| `roi_index` | `100 · Π(1+r_s)` rebased at window start | `null` if no valid day |
| `max_drawdown_pct` | peak-to-trough on `roi_index` | `null` if no index |
| `as_of` | newest wallet data timestamp in group | `null` if no member has data |
| `evaluated_at` | request evaluation time (UTC) | always present |
| `cumulative_realized_pnl_t` | `Σ_{d≤t} realized_pnl(d)` | `null` when no daily rows |
| `open_positions` | `COUNT(trader_positions)` | `0` is valid |
| `global unique wallets` | `COUNT(DISTINCT (venue_id, wallet_address))` over all user groups | — |
| `global account_value` | `Σ latest` over distinct wallets | not sum of group totals |

Rules: never average per-wallet ROI naively; use capital-weighted aggregate; never double-count a wallet in a group; never return `0` for unavailable; include `partial`, `missing_wallets`, `as_of`, `evaluated_at`, `data_status`.

ROI v1.1: money-weighted/IRR using deposits/withdrawals from `userNonFundingLedgerUpdates`; cash flows are normalized to UTC day boundaries; member changes mid-period exclude the missing wallet from affected days and set `partial=true`; `net_cash_flow` remains NULL in `trader_equity_daily` (upstream limitation) but ledger flows are tracked separately for ROI.

Data status (group): `empty` (0 members) · `error` (no member has usable data) · `stale` (newest member data > 24h) · `partial` (some members not fresh/complete) · `ready` (all members ready). Member ready = `sync_status='ready'` and positions synced ≤ 15 min.

## 5. API conventions

- Prefix `/v1`, JWT required, owner-scoped: unknown group → 404, foreign → 403.
- Envelope: `{ "success": true, "data": ..., "meta": ... }`; errors `{ "success": false, "error": { "code", "message" } }`.
- Timestamps RFC3339 UTC; ids UUID; money as JSON numbers; unavailable = `null`.
- List meta: page mode `{page, total_pages, total, limit}`; cursor mode `{limit, has_more, cursor}`.
- Page size 1..100 (default 20). Cursor = HMAC-sealed keyset (same pattern as trader activity).
- Sorting whitelist per endpoint; invalid → `INVALID_FILTER`.
- New error codes: `ALERT_NOT_FOUND`, `ALERT_DUPLICATE`, `ALERT_LIMIT`, `INVALID_ALERT_TYPE`, `INVALID_ALERT_PARAMS`, `COMPARE_TOO_FEW`, `COMPARE_TOO_MANY`, `INVALID_PERIOD`.

## 6. Endpoints

### 6.1 Groups overview

`GET /v1/trader-groups`

| Query | Type | Default |
|---|---|---|
| `search` | string (name/description, ILIKE) | — |
| `favorite` | bool | — |
| `status` | `empty\|ready\|partial\|stale\|error` | — |
| `period` | `1D\|7D\|30D\|90D\|ALL` | `30D` |
| `sort` | `name\|account_value\|unrealized_pnl\|roi\|volume\|open_positions\|win_rate\|updated_at\|created_at` | `name` |
| `dir` | `asc\|desc` | `asc` |
| `page` / `page_size` | int | 1 / 20 |

Ordering: `is_favorite DESC` always first, then requested sort, then `name ASC` tiebreak.

`data`: `GroupListItem[]` (array), with `meta: { page, total_pages, total, limit }` pagination.

```json
{
  "id": "uuid", "name": "string", "description": "string", "is_favorite": true,
  "notify_enabled": false, "member_count": 12,
  "account_value": 1234.5, "unrealized_pnl": -12.3, "roi": 0.0431,
  "volume": 999.0, "open_positions": 4, "win_rate": 0.53,
  "updated_at": "2026-11-23T10:00:00Z",
  "data_status": "ready", "partial": false, "missing_wallets": 0
}
```

List response follows the standard envelope: data is an array; pagination is in meta. No top-level as_of on list (per-row updated_at + overview endpoint provide freshness).

`GET /v1/trader-groups/overview?period=30D` → `{ total_groups, unique_wallets, account_value, unrealized_pnl, partial, missing_wallets, stale_groups, empty_groups, as_of }`

### 6.2 Group CRUD

| Method | Path | Body | Data |
|---|---|---|---|
| POST | `/v1/trader-groups` | `{name, description?}` | Group |
| GET | `/v1/trader-groups/{id}` | — | Group + `data_status`, `as_of` |
| PATCH | `/v1/trader-groups/{id}` | `{name?, description?, is_favorite?, notify_enabled?}` | Group |
| DELETE | `/v1/trader-groups/{id}` | — | 204 |
| POST | `/v1/trader-groups/{id}/duplicate` | `{name?}` | Group (members copied, `is_favorite=false`) |
| PATCH | `/v1/trader-groups/{id}/favorite` | `{is_favorite: bool}` | `{id, is_favorite}` |

Group: `{id, user_id, name, description, is_favorite, notify_enabled, member_count, created_at, updated_at}`.

`notify_enabled` is included — fixes the existing mismatch where the FE writes it but the BE ignores it.

### 6.3 Members

`GET /v1/trader-groups/{id}/members?period&search&sort&dir&page&page_size&filter`

`filter`: `all|missing_data|stale|in_other_groups`. `sort`: `wallet_address|account_value|unrealized_pnl|roi|volume|open_positions|last_trade_at|updated_at`.

```json
{
  "venue_id": "uuid", "venue": "hyperliquid", "wallet_address": "0x…",
  "display_name": "string|null", "alias": "string|null", "note": "string|null",
  "account_value": 100.0, "unrealized_pnl": 1.0, "roi": 0.02,
  "volume": 500.0, "open_positions": 2,
  "last_trade_at": "…|null", "updated_at": "…", "data_status": "ready",
  "in_groups": [{ "id": "uuid", "name": "string" }]
}
```

`POST` / `DELETE` / `PATCH .../members` keep existing bodies; add `skipped` to POST result and write `group_membership_events`. Removing membership never deletes registry rows.

### 6.4 Group stats

`GET /v1/trader-groups/{id}/stats?period=1D|7D|30D|90D|ALL`

```json
{
  "period": "30D", "from": "2026-10-25", "to": "2026-11-23",
  "as_of": "…", "evaluated_at": "…", "data_status": "partial", "partial": true,
  "members_total": 12, "members_with_data": 10, "missing_wallets": 2,
  "account_value": 1234.5, "total_notional": 800.0,
  "unrealized_pnl": -12.3, "realized_pnl": 44.0, "roi": 0.0431,
  "win_count": 21, "loss_count": 5, "breakeven_count": 2, "trade_count": 28,
  "win_rate": 0.8077, "volume": 5000.0, "profit_factor": 1.8,
  "max_drawdown_pct": 7.2, "open_positions": 4,
  "definitions_version": 2,
  "wallets": [
    { "wallet_address": "0x…", "venue": "hyperliquid", "data_status": "ready",
      "as_of": "…", "has_positions": true, "has_equity": true, "missing_reason": null }
  ]
}
```

### 6.5 Performance series

`GET /v1/trader-groups/{id}/performance?period=…`

```json
{
  "period": "30D", "granularity": "daily", "from": "…", "to": "…",
  "partial": false, "missing_wallets": 0, "insufficient_data": false,
  "metric_availability": { "account_value": true, "cumulative_realized_pnl": true, "roi": true },
  "points": [
    { "t": "2026-11-01T00:00:00Z", "account_value": 1000.0,
      "cumulative_realized_pnl": 12.0, "roi_index": 101.2 }
  ]
}
```

- `1D`: `granularity="intraday"` from `trader_equity_intraday` (5-min points), `cumulative_realized_pnl=null` (not available intraday).
- `7D/30D/90D/ALL`: `granularity="daily"` from `trader_equity_daily` + `trader_daily_stats`.
- `<2` points → `insufficient_data=true`; the API still returns the points it has.
- Charts must show the collection start date when data begins later than `from`.

### 6.6 Positions

`GET /v1/trader-groups/{id}/positions`

```json
{
  "as_of": "…", "data_status": "ready", "partial": false, "missing_wallets": 0,
  "totals": { "long_ntl": 500, "short_ntl": 300, "net_ntl": 200, "gross_ntl": 800, "open_positions": 6 },
  "positions": [{
    "coin": "BTC", "long_ntl": 500, "short_ntl": 0, "net_ntl": 500, "gross_ntl": 500,
    "wallet_count": 2, "unrealized_pnl": 12.0,
    "wallets": [{
      "wallet_address": "0x…", "display_name": "string|null", "alias": "string|null",
      "side": "LONG", "size": 1.2,
      "entry_price": 60000, "mark_price": 61000, "leverage": 3,
      "liquidation_price": 55000, "distance_to_liquidation_pct": 9.84,
      "position_value": 73200, "unrealized_pnl": 1200,
      "as_of": "…", "stale": false
    }]
  }]
}
```

`net_ntl = long_ntl − short_ntl` (signed); `gross_ntl = long + short`. Aggregated by coin over distinct wallets.

### 6.6.1 Exposure

`GET /v1/trader-groups/{id}/exposure?period=1D|7D|30D|90D|ALL`

```json
{
  "period": "30D", "as_of": "…", "data_status": "ready", "partial": false,
  "missing_wallets": 0,
  "rows": [{
    "coin": "BTC",
    "long_ntl": 500.0, "short_ntl": 300.0, "net_ntl": 200.0, "gross_ntl": 800.0,
    "long_wallets": 2, "short_wallets": 1,
    "avg_leverage": 3.5
  }]
}
```

`net_ntl = long_ntl − short_ntl` (signed); `gross_ntl = long + short`. `avg_leverage` is position-value-weighted average leverage over distinct wallets with leverage; `null` if no leverage data. Aggregated by coin over distinct wallets. `period` affects only as_of/freshness, not position notional.

### 6.7 Activity

`GET /v1/trader-groups/{id}/activity?limit=20&cursor&wallet&asset&type&from&to`

```json
{
  "items": [{
    "id": "event-id", "type": "position_closed", "occurred_at": "…",
    "venue": "hyperliquid", "wallet_address": "0x…", "coin": "BTC",
    "size_delta": -1.2, "price": 61000, "realized_pnl": 120.0,
    "meta": {}
  }],
  "next_cursor": "…|null", "has_more": false,
  "partial": false, "sources": { "trades": true, "fills": true, "transfers": false }
}
```

Sources: `trader_trades` (closed), `trader_wallet_event` (opened/increased/reduced, 60d backfill), `group_membership_events`, `group_alert_events`; deposits/withdrawals via on-demand transfers only for groups ≤10 wallets, cached 60s, otherwise `partial=true` and omitted. Dedup: one event per underlying wallet event (`dedup_key` unique).

### 6.8 Comparison

`POST /v1/trader-groups/compare` `{ "group_ids": ["uuid", "uuid"], "period": "30D" }`

```json
{
  "period": "30D",
  "groups": [{ "id": "uuid", "name": "string", "member_count": 3, "stats": { } }],
  "series": [{ "group_id": "uuid", "points": [{ "t": "…", "roi_index": 101.2 }] }],
  "overlap": { "pairwise": [{ "a": "uuid", "b": "uuid", "shared_wallets": 2 }] },
  "warnings": ["groups share wallets; results are not independent portfolios"],
  "as_of": "…"
}
```

2..10 groups, same period/methodology; never sum into a combined total.

### 6.9 Alerts

| Method | Path | Purpose |
|---|---|---|
| GET | `/v1/trader-groups/{id}/alerts` | list alerts + `unread_events` |
| POST | `/v1/trader-groups/{id}/alerts` | create |
| PATCH | `/v1/trader-alerts/{alertId}` | update / enable / disable |
| DELETE | `/v1/trader-alerts/{alertId}` | delete |
| GET | `/v1/trader-groups/{id}/alert-events?unread&limit&cursor` | history |
| POST | `/v1/trader-alert-events/{eventId}/read` | mark read |
| POST | `/v1/trader-groups/{id}/alert-events/read-all` | mark all |
| GET | `/v1/trader-alerts/unread-count` | nav badge |

Alert params:

| Type | Params |
|---|---|
| `unrealized_pnl_above` / `_below` | `{threshold_usd}` |
| `realized_pnl_reaches` / `roi_reaches` | `{threshold, direction: above\|below}` |
| `position_opened` / `_closed` / `_increased` / `_reduced` | `{coin?}` |
| `exposure_above` | `{metric: gross\|net\|long\|short, threshold_usd}` |
| `long_short_shift` | `{window: 1D\|7D\|30D, threshold_pct}` |
| `wallet_inactive` | `{days}` |
| `wallet_stale` | `{minutes}` |
| `wallet_added` / `_removed` | `{}` |

Evaluation:
- Ticker 60s, only `enabled` alerts.
- Threshold alerts: trigger only on `met && !last_condition_met`; persist `last_condition_met` + `last_value` every tick.
- Event alerts: consume `trader_wallet_event` / `trader_trades` / `group_membership_events` since `last_evaluated_at`, dedup by event id.
- `dedup_key = alert_id|subject|event_identity`; unique index prevents duplicates.
- Cooldown per alert+subject using `cooldown_seconds`.
- Dispatch: always insert `group_alert_events` when `notify_in_app`; if `notify_telegram` and group `notify_enabled` and Telegram binding active, enqueue existing notify outbox with `dedup_key`.

## 7. Authorization & transactions

- Every endpoint requires JWT; group ownership verified (404/403); alert ownership via its group; compare validates all ids.
- Membership add/remove/update and duplicate run in a transaction; membership events written in the same transaction.
- Favorite toggle is idempotent.

## 8. Caching & performance

- Single SQL per list/stats using CTEs; no N+1; member list paginated server-side.
- 30s in-memory cache keyed `(user_id, period, filter-hash)` for list/stats/overview and `(user_id, sorted group ids, period)` for compare; invalidated by a generation counter on group/membership/favorite/alert mutations.
- Required indexes: `trader_daily_stats (venue_id, wallet_address, stat_date)` exists; `trader_equity_daily` PK exists; `trader_group_members (venue_id, wallet_address)` exists; new indexes in §3.
- Activity keyset pagination `(occurred_at DESC, id DESC)`.

## 9. Frontend contract (in `/groups`)

- `/groups`: header (title, Create, Compare), 4 compact stats, toolbar (search/status/sort), favorites block, all-groups table with actions menu.
- `/groups/[groupId]`: header (favorite, status, actions: edit/add wallets/export/settings/delete) + tabs `Overview | Positions | Members | Activity | Alerts` (deep-link via `?tab=overview|positions|members|activity|alerts`).
- `/groups/compare`: multi-select (2–10), range `7D|30D|90D|ALL`, comparative ROI-index chart, metrics table, remove group, best/worst highlight.
- Reuse `TraderTable` patterns, `Pagination`, `ScannerToolbar`, skeletons, dialogs, tooltips. Charts: hand-rolled SVG following `portfolio-chart.tsx`; no new chart dependency.
- Export CSV is client-side from already-loaded data (no API).
- Partial/stale/unavailable states must be rendered from `partial`, `missing_wallets`, `data_status`, `insufficient_data`.

## 10. Testing requirements

Unit (Go): TWR chain, money-weighted ROI, max drawdown, win rate, profit factor, favorite ordering, membership idempotency, overlap aggregation, alert threshold crossing, dedup, cooldown, comparison, exposure aggregation, liquidation distance, ledger parsing.
Handler (Go): every endpoint: auth, validation, 404/403, invalid period/sort/alert params, pagination.
Integration (Go): migrations; CRUD; many-to-many; wallet shared by 2 groups reflects updates in both; remove membership keeps registry; analytics SQL; alert persistence/idempotency; authz.
FE unit: table rendering, favorite ordering, chart empty/insufficient, alert form validation, compare selection, net exposure rendering, liquidation warning, win-rate/PF formatting.
E2E: create group → add wallets → same wallet in 2 groups → favorite persist/order → detail stats → chart range → compare → create alert + seeded trigger → delete group leaves wallets → partial/stale states.
Critical: Wallet A in Group 1 and Group 2; update A → both groups reflect; A collected once; global unique count = 1.

## 11. Acceptance criteria

1. Group CRUD + duplicate + favorite persist.
2. Wallet ↔ group many-to-many; removal never deletes wallets.
3. Overview stats use distinct wallets; partial/stale flagged.
4. Group stats/positions use shared wallet data; no double counting.
5. Positions aggregated by coin with long/short/net/gross and wallet drill-down.
6. Performance chart uses real historical data; 1D only after intraday collection starts, clearly labelled.
7. Comparison works for 2–10 groups with shared-wallet warning.
8. Alerts: crossing-based, deduped, cooled down, history + read state.
9. Partial/stale/unavailable rendered explicitly; no fabricated zeros.
10. Wallet Detail functionality unchanged.
11. All unit/handler/integration/e2e tests pass; visual snapshots approved.
12. No per-group collectors or WS subscriptions; no duplicated wallet records.

## 12. Known limitations (must be surfaced in UI/docs)

- Deposits/withdrawals are not tracked (`net_cash_flow` NULL) → ROI is flow-unadjusted; intraday flows can distort daily returns.
- 1D chart starts at intraday collection deployment date; before that `insufficient_data`.
- Activity `position_opened/increased/reduced` backfill limited to 60d (`trader_fill_buffer`); closed trades 60d.
- Transfers (deposit/withdraw) fetched on demand for groups ≤10 wallets; larger groups return `partial`.
- Alerts are in-app + Telegram only (existing infra); no email/Discord.
- Group totals are per-group; no combined portfolio total across groups (overlap makes it invalid).

## 13. Rollout

1. Contract approval.
2. Migration + repo/service + handler with tests (BE).
3. FE API client/types + pages.
4. Contract tests + visual QA.
5. Shadow compare old/new group stats on 10 real groups before enabling alerts.

## 14. Changelog

- v1.0 (2026-11-23): initial approved draft.
- v1.1 (2026-11-26): ROI money-weighted/IRR with ledger cash flows; per-wallet TWR aggregate; win/loss/breakeven counts; PF null rules; freshness fields; exposure endpoint; positions liquidation fields; definitions_version=2. Pending approval.
