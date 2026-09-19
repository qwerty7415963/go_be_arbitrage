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

### Unified State
| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/unified/instruments` | All unified instrument states |
| GET | `/api/v1/unified/instruments/:id` | Unified state for instrument |
| WS | `/api/v1/unified/ws` | Real-time unified state updates |

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

## Swagger

Swagger UI available at `http://localhost:8080/swagger/index.html`

Regenerate docs:
```bash
swag init -g cmd/server/main.go -o docs
```
