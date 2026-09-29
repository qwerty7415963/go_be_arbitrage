# go_be_arbitrage

Go modular monolith arbitrage platform backend supporting CEX ↔ CEX, CEX ↔ Perp DEX, Perp DEX ↔ Perp DEX arbitrage.

## Quick Start

```bash
# Start PostgreSQL (Docker)
docker-compose -f docker-compose.test.yml up -d

# Run migrations
make migrate-up

# Start server
go run ./cmd/server
```

Server runs on `http://localhost:8080` by default.

## API Endpoints

### Auth
| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/v1/auth/register` | Register new user |
| POST | `/api/v1/auth/login` | Login |
| POST | `/api/v1/auth/refresh` | Refresh JWT token |
| POST | `/api/v1/auth/logout` | Logout (requires JWT) |
| GET | `/api/v1/auth/me` | Get current user (requires JWT) |

### Auth — Web3 Wallet (EIP-4361)
| Method | Path | Auth | Description |
|--------|------|------|-------------|
| POST | `/api/v1/auth/wallet/nonce` | Public | Generate nonce for wallet signing |
| POST | `/api/v1/auth/wallet/verify` | Public | Verify SIWE signature → JWT (auto-create user) |
| POST | `/api/v1/auth/wallet/link` | JWT | Link wallet to existing account |
| DELETE | `/api/v1/auth/wallet/:wallet_id` | JWT | Unlink wallet from account |
| GET | `/api/v1/auth/wallet/list` | JWT | List user's linked wallets |

### Venues (Admin only)
| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/venues` | List venues |
| POST | `/api/v1/venues` | Create venue |
| GET | `/api/v1/venues/:id` | Get venue |
| PUT | `/api/v1/venues/:id` | Update venue |
| DELETE | `/api/v1/venues/:id` | Delete venue |

### Instruments
| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/instruments` | List instruments |
| GET | `/api/v1/instruments/tradable` | List tradable instruments |
| GET | `/api/v1/instruments/:id` | Get instrument |

### Market Data
| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/market/trades` | Recent trades |
| GET | `/api/v1/market/ticker` | Latest ticker |
| GET | `/api/v1/market/funding` | Latest funding rate |

### Order Book
| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/orderbook/depth` | L2 order book depth |
| GET | `/api/v1/orderbook/health` | Order book health status |
| GET | `/api/v1/orderbook/tradable` | Check if order book is tradable |
| POST | `/api/v1/orderbook/resync` | Request order book resync |
| **WS** | **`/api/v1/orderbook/ws`** | **Real-time order book updates** |

### Funding Arbitrage
| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/funding/arbitrage` | Funding arbitrage table |

> Pairs appear only for instruments mapped ACTIVE on **both** venues
> (`venue_instruments`) with **fresh** funding (<15 min, else `include_stale=true`).
> Funding sources: Binance, Extended, Variational (WS/REST adapters) +
> Hyperliquid (REST `metaAndAssetCtxs` poll, hourly) via the
> background collector.

#### Query Parameters

| Param | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `venue_id` | UUID | Yes | — | Venue IDs (min 2, max 10), repeated or comma-separated |
| `sort` | string | No | `rate_8h_desc` | Sort by: `rate_1h_desc`, `rate_8h_desc`, `apr_desc`, `spread_desc` |
| `page` | int | No | 1 | Page number (takes priority over offset) |
| `limit` | int | No | 50 | Items per page (1-200) |
| `offset` | int | No | 0 | Offset (alternative to page) |
| `include_stale` | bool | No | false | Include stale funding data |
| `refresh` | bool | No | false | Force cache refresh |

#### Response Meta

```json
{
  "page": 1,
  "total_pages": 3,
  "limit": 50,
  "offset": 0,
  "has_more": true
}
```

### Unified State
| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/unified/instruments` | All unified instrument states |
| GET | `/api/v1/unified/instruments/:id` | Unified state for instrument |
| WS | `/api/v1/unified/ws` | Real-time unified state updates |

### Wallet Groups (JWT required; per-user ownership)
| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/v1/groups` | Create group (201; blank name → 400, duplicate → 409 `GROUP-002`) |
| GET | `/api/v1/groups` | List current user's groups (with `wallet_count`) |
| GET | `/api/v1/groups/:id` | Get group with `wallet_count` |
| PATCH | `/api/v1/groups/:id` | Update name/description/color |
| DELETE | `/api/v1/groups/:id` | Delete group (memberships removed, wallets kept) |
| POST | `/api/v1/groups/:id/wallets` | Add wallets — IDs or addresses, idempotent (`WALLET-001` unknown); 200 `{"added": N, "skipped": M}` (failures abort the batch as errors, never partial) |
| DELETE | `/api/v1/groups/:id/wallets` | Remove wallets — idempotent no-op |
| GET | `/api/v1/groups/:id/wallets` | List wallets — always `GroupWallet[]` (every Wallet field + `added_at`); `include=metrics` or any scanner filter → metric-enriched rows, else `metrics` null (BE-09) |

Group mutations (create/update/delete/add/remove wallets) emit structured logs
with `actor` (user id) + `request_id` (BE-13); IDs and counts only, never secrets.

### Wallet Scanner (JWT required; TEST-01 grammar)
| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/wallets` | Offset-paginated scanner: filters, timeframe, metric operators, sort, watchlist star filter |
| GET | `/api/v1/wallets/filter-config` | Filter config for dynamic UI: `dexes`/`chains`/`markets` (data-driven) + `timeframes`/`sort_fields`/`metrics[]` (code tables, with `min`/`max`/`ops`/`sortable`) |
| GET | `/api/v1/wallets/:id` | Wallet detail: identity + timeframe metrics + per-market `positions[]` (empty array when none) + own group memberships only |
| PATCH | `/api/v1/wallets/:id` | Update caller's private tag and/or watchlist star (`{"tag": "..."}` empty clears, max 100 chars; `{"watchlisted": true}` star, `false` unstar; at least one field, both allowed) |

Scanner query grammar (shared by both endpoints):

```
search=0xabc                    # partial address OR own tag (case-insensitive)
dex=hyperliquid,gmx             # multi-select OR: repeat key or comma form;
                                #   data-driven enum (ACTIVE venues in DB), unknown → COMMON-902
chain=evm&chain=starknet        # same for chain (observed chains) / market (ingested markets)
watchlisted=true                # caller's own watchlist stars: true = starred only,
                                #   false = unstarred only; absent = no filter
timeframe=24H|7D|30D|90D|ALL    # default 30D; unavailable metrics = null, never 0 (BR-07)
start=2026-01-01T00:00:00Z&end=2026-02-01T00:00:00Z   # custom range (RFC3339, start<end)
pnl_gt=1000                     # every metric × every op: _gt _gte _lt _lte _between (lo,hi)
win_rate_gte=60                 # metrics: pnl roi win_rate volume trade_count
long_short_ratio_gt=1.5         #          avg_position avg_leverage long_short_ratio
last_active_within=24h          # or last_active_from / last_active_to (RFC3339)
sort=pnl&order=desc             # sort: pnl roi win_rate volume trade_count
                                #       avg_position avg_leverage last_active (default pnl desc)
page=1&limit=50                 # offset pagination, limit max 200
```

Response `meta` carries `total` (full count) alongside `total_pages`/`has_more`.
Each row's `metrics.computed_at` is the snapshot computation time (freshness;
null when the wallet has no snapshot for the timeframe).

Invalid enum/operator/sort/timeframe → 400 `COMMON-902`. Numeric filters AND
across metrics; multi-select OR within a key (BR-11). Null metrics never match
numeric filters (BR-07). Ordering is metric-first with a deterministic
`(chain, address)` tiebreak (BR-12).

### Metric Ingestion — Hyperliquid (background worker)
| Item | Description |
|------|-------------|
| Source | Hyperliquid public `POST /info` (`userFillsByTime`, no auth, any address) |
| Scope | EVM tracked wallets (`chain='evm'`); snapshots per timeframe 24H/7D/30D/90D/ALL |
| Mapping | `closedPnl − fee` → net realized PnL; `dir`+`startPosition` → logical-position legs; leverage unavailable → `avg_leverage` null |
| Schedule | On startup + every 6h (`runHyperliquidBackfill`); per-wallet failures logged, retried next tick |
| Limits | ≤2000 fills/response (auto window-split); only 10,000 most recent fills queryable → capped snapshots flagged `is_partial` |
| Spike note | Extended has no by-address endpoint (self-scoped feeds only); Variational trading API not live — see `WALLET_DASHBOARD_PLAN.md §8` |

## WebSocket: Order Book Real-time

### Connect

```javascript
const venueID = "your-venue-uuid";
const instrumentID = "your-instrument-uuid";
const ws = new WebSocket(
  `ws://localhost:8080/api/v1/orderbook/ws?venue_id=${venueID}&instrument_id=${instrumentID}&depth=10`
);
```

### Query Parameters

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `venue_id` | UUID | Yes | Venue ID |
| `instrument_id` | UUID | Yes | Instrument ID |
| `depth` | int | No | Depth levels (default 10, max 20) |

### Message Format

**Initial snapshot** (sent on connect):
```json
{
  "type": "snapshot",
  "data": {
    "venue_id": "574026f5-c6be-4447-be58-c1c978e80f76",
    "instrument_id": "...",
    "sequence": 12345,
    "best_bid": "50000.00",
    "best_ask": "50001.00",
    "bids": [
      {"price": "50000.00", "quantity": "1.5"},
      {"price": "49999.00", "quantity": "2.0"}
    ],
    "asks": [
      {"price": "50001.00", "quantity": "0.8"},
      {"price": "50002.00", "quantity": "1.2"}
    ],
    "timestamp": "2026-09-16T10:00:00Z"
  }
}
```

**Real-time updates** (pushed when order book changes):
Same format as above with updated sequence and price levels.

### Client Example

```javascript
const ws = new WebSocket(
  `ws://localhost:8080/api/v1/orderbook/ws?venue_id=${venueID}&instrument_id=${instrumentID}&depth=10`
);

ws.onopen = () => {
  console.log("Connected to orderbook stream");
};

ws.onmessage = (event) => {
  const msg = JSON.parse(event.data);
  if (msg.type === "snapshot") {
    updateOrderbookUI(msg.data);
  }
};

ws.onclose = () => {
  console.log("Disconnected, reconnecting...");
  setTimeout(reconnect, 3000);
};
```

## Architecture

```
Exchange WS Feeds → Adapters → Bridge → Engine → Service → WS Handler → Clients
                                    ↓
                              PostgreSQL (replay/audit)
```

- **Adapters**: Exchange-specific WebSocket handlers (Binance, Extended)
- **Bridge**: Converts exchange events to canonical format, feeds engine
- **Engine**: L2 order book state management (snapshot/delta, sequence validation)
- **Service**: Business logic layer with pub/sub
- **WS Handler**: WebSocket endpoint for real-time client streaming

## Web3 Wallet Auth (EIP-4361)

### Flow

```
Frontend                          Backend                         DB
  │                                 │                               │
  ├──POST /wallet/nonce────────────►│──generate nonce──────────────►│
  │  {address, chain_id}            │──store with TTL───────────────►│
  │◄──{nonce, message}─────────────│                               │
  │                                 │                               │
  │  [user signs in wallet]         │                               │
  │                                 │                               │
  ├──POST /wallet/verify───────────►│──parse SIWE message───────────│
  │  {message, signature}           │──verify EIP-191 signature─────│
  │                                 │──validate nonce───────────────│
  │                                 │──find or create user──────────►│
  │                                 │──issue JWT────────────────────│
  │◄──{access_token,refresh,user}───│                               │
```

### Supported Chains

| Chain ID | Network |
|----------|---------|
| 1 | Ethereum |
| 42161 | Arbitrum One |
| 10 | Optimism |
| 137 | Polygon |
| 8453 | Base |

### Environment Variables

```bash
ARBITRAGE_SIWE_DOMAIN=localhost        # SIWE domain validation
ARBITRAGE_SIWE_NONCE_TTL=5m            # Nonce expiry (default 5m)
ARBITRAGE_SIWE_CHAINS=1,42161,10,137,8453  # Supported chain IDs
```

## Development

```bash
# Run tests
go test ./...

# Run specific package tests
go test ./internal/orderbook/...

# Build
go build ./...

# Lint
golangci-lint run
```

## Database Migrations

Migrations live in `migrations/` (embedded, `NNNNNN_name.up.sql` / `.down.sql`) and run via golang-migrate through the server binary:

```bash
make db-migrate-up                 # apply all pending migrations
make db-migrate-down STEPS=1       # roll back last migration (or STEPS=all)
make db-migrate-version            # show current version
make db-migrate-create name=add_x  # new migration pair (requires migrate CLI)
```

Direct usage: `go run ./cmd/server migrate <up|down [N|all]|version|force <V>|goto <V>>`.
The `DB_URL`/config Postgres URL is used as target. Existing databases without a
`schema_migrations` table must be baselined once: `go run ./cmd/server migrate force <latest>`.

Integration tests for migrations: `go test -tags=integration -run TestMigrate ./internal/database/...` (uses a dedicated `arbitrage_migrate_test` database).

## Swagger

Swagger UI available at `http://localhost:8080/swagger/index.html`

Regenerate docs:
```bash
swag init -g cmd/server/main.go -o docs
```
