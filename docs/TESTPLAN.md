# TESTPLAN.md — Comprehensive Test Plan

> Every feature MUST have test cases documented here BEFORE writing code.
> See AGENTS.md for mandatory workflow.

---

## 1. Auth Module (`internal/auth/`)

### 1.1 Email/Password — Unit Tests

| Case | Function | Scenario | Input | Expected |
|------|----------|----------|-------|----------|
| AUTH-U-01 | Register | Success | Valid email + password | User created, JWT + refresh returned |
| AUTH-U-02 | Register | Duplicate email | Existing email | COMMON-904 Conflict |
| AUTH-U-03 | Register | Invalid email format | "not-email" | COMMON-902 Validation |
| AUTH-U-04 | Register | Short password | "ab" | COMMON-902 Validation |
| AUTH-U-05 | Login | Success | Correct credentials | JWT + refresh returned |
| AUTH-U-06 | Login | Non-existent email | Unknown email | AUTH-001 Invalid credentials |
| AUTH-U-07 | Login | Wrong password | Wrong password | AUTH-001 Invalid credentials |
| AUTH-U-08 | Login | Disabled user | User status=DISABLED | AUTH-006 Disabled |
| AUTH-U-09 | Refresh | Success | Valid refresh token | New JWT, old token revoked |
| AUTH-U-10 | Refresh | Token not found | Random token | AUTH-003 Invalid token |
| AUTH-U-11 | Refresh | Token revoked | Already revoked token | AUTH-004 Revoked |
| AUTH-U-12 | Refresh | Token expired | Expired token | AUTH-002 Expired |
| AUTH-U-13 | Logout | Success | Valid user ID | All refresh tokens revoked |
| AUTH-U-14 | ChangePassword | Success | Correct old + valid new | Hash updated, all tokens revoked |
| AUTH-U-15 | ChangePassword | Wrong old password | Wrong old password | AUTH-001 Invalid credentials |
| AUTH-U-16 | ChangePassword | Short new password | "ab" | COMMON-902 Validation |
| AUTH-U-17 | GetUser | Success | Existing user ID | User profile returned |
| AUTH-U-18 | GetUser | Not found | Random UUID | COMMON-903 Not found |
| AUTH-U-19 | EnsureAdmin | Empty email | email="" | nil (skip) |
| AUTH-U-20 | EnsureAdmin | Empty password | password="" | nil (skip) |

### 1.2 Token Operations — Unit Tests

| Case | Function | Scenario | Expected |
|------|----------|----------|----------|
| AUTH-T-01 | GenerateTokenPair | Admin role | Claims.Role == "admin" |
| AUTH-T-02 | GenerateTokenPair | Empty role defaults to "user" | Claims.Role == "user" |
| AUTH-T-03 | GenerateTokenPair | ExpiresAt in future | ExpiresAt > now |
| AUTH-T-04 | GenerateTokenPair | Different users produce different tokens | token1 != token2 |
| AUTH-T-05 | ValidateToken | Correct claims | All fields match |
| AUTH-T-06 | ValidateToken | Wrong secret | Error |
| AUTH-T-07 | ValidateToken | Expired token | Error |
| AUTH-T-08 | ValidateToken | Invalid format | Error |
| AUTH-T-09 | ValidateToken | Empty string | Error |
| AUTH-T-10 | ValidateToken | Tampered payload | Error |
| AUTH-T-11 | ValidateToken | Concurrent access (100 goroutines) | No errors |
| AUTH-T-12 | hashToken | Deterministic | Same input = same hash |
| AUTH-T-13 | hashToken | Different inputs | Different hashes |
| AUTH-T-14 | hashToken | Length | 64 char hex |
| AUTH-T-15 | generateRefreshToken | Uniqueness | Two calls = different tokens |
| AUTH-T-16 | generateRefreshToken | Length | 64 char hex |

### 1.3 Handler Tests (existing — DB-backed)

| Case | Endpoint | Scenario | Expected |
|------|----------|----------|----------|
| AUTH-H-01 | POST /register | Success | 201 + JWT |
| AUTH-H-02 | POST /register | Duplicate email | 409 |
| AUTH-H-03 | POST /register | Invalid body | 400 |
| AUTH-H-04 | POST /register | Short password | 400 |
| AUTH-H-05 | POST /register | Invalid email | 400 |
| AUTH-H-06 | POST /login | Success | 200 + JWT |
| AUTH-H-07 | POST /login | Wrong password | 400 |
| AUTH-H-08 | POST /login | Non-existent email | 400 |
| AUTH-H-09 | POST /refresh | Success | 200 + new JWT |
| AUTH-H-10 | POST /refresh | Reuse old token | 401 |
| AUTH-H-11 | POST /refresh | Expired token | 401 |
| AUTH-H-12 | POST /logout | No token | 403 |
| AUTH-H-13 | POST /logout | Refresh dies after logout | 401 |
| AUTH-H-14 | POST /change-password | Success | 204 |
| AUTH-H-15 | POST /change-password | Wrong old password | 400 |
| AUTH-H-16 | GET /me | Success | 200 + user data |
| AUTH-H-17 | GET /me | No auth | 403 |

### 1.4 Web3 Wallet — Unit Tests

| Case | Function | Scenario | Expected |
|------|----------|----------|----------|
| W3-U-01 | isValidEthAddress | Valid 0x + 40 hex | true |
| W3-U-02 | isValidEthAddress | Too short | false |
| W3-U-03 | isValidEthAddress | No 0x prefix | false |
| W3-U-04 | isValidEthAddress | Invalid hex chars | false |
| W3-U-05 | isValidEthAddress | Empty string | false |
| W3-U-06 | normalizeAddress | Mixed case → lowercase | "0xABC" → "0xabc" |
| W3-U-07 | isChainSupported | Supported chain (1) | true |
| W3-U-08 | isChainSupported | Unsupported chain (42) | false |
| W3-U-09 | GenerateNonce | Valid address + chain | Nonce 32 chars, expires > now |
| W3-U-10 | GenerateNonce | Unsupported chain | AUTH-016 |
| W3-U-11 | GenerateNonce | Invalid address | COMMON-902 |
| W3-U-12 | VerifySignature | Invalid SIWE format | AUTH-011 |
| W3-U-13 | VerifySignature | Domain mismatch | AUTH-011 |
| W3-U-14 | VerifySignature | Unsupported chain | AUTH-016 |
| W3-U-15 | VerifySignature | Message expired | AUTH-002 |
| W3-U-16 | VerifySignature | Nonce not found | AUTH-012 |
| W3-U-17 | VerifySignature | Nonce already used | AUTH-013 |
| W3-U-18 | VerifySignature | Invalid signature | AUTH-011 |
| W3-U-19 | VerifySignature | Address mismatch | AUTH-011 |
| W3-U-20 | LinkWallet | Invalid SIWE format | AUTH-011 |
| W3-U-21 | LinkWallet | Domain mismatch | AUTH-011 |
| W3-U-22 | LinkWallet | Chain_id mismatch | AUTH-011 |
| W3-U-23 | LinkWallet | Address mismatch | AUTH-011 |
| W3-U-24 | LinkWallet | Message expired | AUTH-002 |
| W3-U-25 | LinkWallet | Wallet owned by another user | AUTH-014 |
| W3-U-26 | LinkWallet | Already linked (idempotent) | nil |
| W3-U-27 | LinkWallet | Valid link | Wallet created |
| W3-U-28 | UnlinkWallet | Not owned | AUTH-015 |
| W3-U-29 | UnlinkWallet | Last wallet | COMMON-902 |
| W3-U-30 | UnlinkWallet | Valid | nil |
| W3-U-31 | ListWallets | No wallets | Empty array |
| W3-U-32 | ListWallets | Multiple wallets | Return all, primary first |

### 1.5 Web3 Handler Tests

| Case | Endpoint | Scenario | Expected |
|------|----------|----------|----------|
| W3-H-01 | POST /wallet/nonce | Valid address + chain | 200 + nonce |
| W3-H-02 | POST /wallet/nonce | Invalid address | 400 |
| W3-H-03 | POST /wallet/nonce | Unsupported chain | 400 |
| W3-H-04 | POST /wallet/verify | Valid signature | 200 + JWT |
| W3-H-05 | POST /wallet/verify | Invalid SIWE message | 400 |
| W3-H-06 | POST /wallet/verify | Invalid signature | 401 |
| W3-H-07 | POST /wallet/link | Valid (JWT required) | 200 |
| W3-H-08 | POST /wallet/link | No JWT | 401 |
| W3-H-09 | DELETE /wallet/:id | Valid (JWT required) | 204 |
| W3-H-10 | DELETE /wallet/:id | No JWT | 401 |
| W3-H-11 | DELETE /wallet/:id | Not owned | 400 |
| W3-H-12 | GET /wallet/list | Valid (JWT required) | 200 + wallets |
| W3-H-13 | GET /wallet/list | No JWT | 401 |

### 1.6 Integration Tests — Auth Repository

| Case | Function | Scenario | Expected |
|------|----------|----------|----------|
| AUTH-I-01 | CreateUser | New user | User stored |
| AUTH-I-02 | CreateUser | Duplicate email | Error |
| AUTH-I-03 | GetUserByEmail | Found | User returned |
| AUTH-I-04 | GetUserByEmail | Not found | pgx.ErrNoRows |
| AUTH-I-05 | ExistsByEmail | True | true |
| AUTH-I-06 | ExistsByEmail | False | false |
| AUTH-I-07 | RefreshToken | Lifecycle create→get→revoke→delete | All states correct |
| AUTH-I-08 | DeleteExpired | Removes expired tokens | Count > 0 |
| AUTH-I-09 | UpdateLastLogin | Sets timestamp | Updated |
| AUTH-I-10 | UpdatePasswordHash | Updates hash | New hash stored |

### 1.7 Integration Tests — Web3 Repository

| Case | Function | Scenario | Expected |
|------|----------|----------|----------|
| W3-I-01 | CreateWallet | New wallet | Stored with verified_at |
| W3-I-02 | GetWalletByAddress | Found | Wallet returned |
| W3-I-03 | GetWalletByAddress | Not found | Error |
| W3-I-04 | GetWalletsByUserID | 3 wallets | Return 3, primary first |
| W3-I-05 | DeleteWallet | Existing | Removed |
| W3-I-06 | SetPrimaryWallet | Update | is_primary updated |
| W3-I-07 | IsWalletOwnedByUser | True/False | Correct result |
| W3-I-08 | CreateNonce | New nonce | Stored |
| W3-I-09 | GetValidNonce | Valid | Returned |
| W3-I-10 | GetValidNonce | Expired | Error |
| W3-I-11 | GetValidNonce | Used | Error |
| W3-I-12 | MarkNonceUsed | Set used | used=TRUE |
| W3-I-13 | CreateUserWithWallet | Transaction | Both user + wallet created |
| W3-I-14 | GetUserByAddress | Found | User returned |
| W3-I-15 | GetUserByAddress | Not found | Error |

### 1.8 E2E Tests — Auth

| Case | Flow | Steps | Expected |
|------|------|-------|----------|
| AUTH-E-01 | Register → Login → Me | register → login → GET /me | User data matches |
| AUTH-E-02 | Login → Refresh → Logout | login → refresh → logout → refresh(fail) | Old token dead |
| AUTH-E-03 | Change Password | register → change-password → login old(fail) → login new(ok) | Password updated |
| AUTH-E-04 | Disabled User | register → disable → refresh(fail) | 403 |
| AUTH-E-05 | Admin Seed | EnsureAdmin → login admin | Admin role |
| W3-E-01 | Wallet-first new user | nonce → sign → verify → GET /me | New user created |
| W3-E-02 | Wallet-first returning user | nonce → sign → verify → verify | Same user_id |
| W3-E-03 | Link wallet to email user | register → link → list | Wallet linked |
| W3-E-04 | Multi-wallet + unlink | link A → link B → list(2) → unlink A → list(1) | Wallet removed |
| W3-E-05 | Nonce replay | nonce → verify(ok) → verify(fail) | AUTH-013 |
| W3-E-06 | Nonce expiry | nonce → wait → verify | AUTH-012 |
| W3-E-07 | Cross-chain replay | sign chain 1 → verify chain 42161 | AUTH-011 |
| W3-E-08 | Rate limit | 6x POST /login → 429 | Rate limited |

---

## 2. Venue Module (`internal/venue/`)

### 2.1 Unit Tests (existing — mock repo)

| Case | Function | Scenario | Expected |
|------|----------|----------|----------|
| V-U-01 | Create | Valid | Venue created |
| V-U-02 | Create | Duplicate code | COMMON-904 |
| V-U-03 | GetByID | Found | Venue returned |
| V-U-04 | GetByID | Not found | COMMON-903 |
| V-U-05 | GetByCode | Found | Venue returned |
| V-U-06 | GetByCode | Not found | COMMON-903 |
| V-U-07 | List | Multiple | All returned |
| V-U-08 | Update | Valid | Updated |
| V-U-09 | Delete | Valid | Deleted |
| V-U-10 | Delete | Not found | COMMON-903 |

### 2.2 Handler Tests (existing)

| Case | Endpoint | Scenario | Expected |
|------|----------|----------|----------|
| V-H-01 | POST /venues | Valid | 201 |
| V-H-02 | POST /venues | Invalid body | 400 |
| V-H-03 | POST /venues | Duplicate code | 409 |
| V-H-04 | GET /venues/:id | Found | 200 |
| V-H-05 | GET /venues/:id | Invalid ID | 400 |
| V-H-06 | GET /venues/:id | Not found | 404 |
| V-H-07 | GET /venues | Empty list | 200 + [] |
| V-H-08 | PUT /venues/:id | Valid | 200 |

### 2.3 Integration Tests (existing)

| Case | Function | Scenario | Expected |
|------|----------|----------|----------|
| V-I-01 | Create | Valid | Stored |
| V-I-02 | Create | Duplicate code | Error |
| V-I-03 | List | After create | Returns venue |
| V-I-04 | Update | Change name | Updated |
| V-I-05 | Delete | Remove | Gone |

---

## 3. Instrument Module (`internal/instrument/`)

### 3.1 Unit Tests

| Case | Function | Scenario | Expected |
|------|----------|----------|----------|
| I-U-01 | Create | Valid instrument | Created |
| I-U-02 | Create | Duplicate symbol | COMMON-904 |
| I-U-03 | GetByID | Found | Returned |
| I-U-04 | GetByID | Not found | COMMON-903 |
| I-U-05 | ListTradable | With reviewed+enabled | Filtered list |
| I-U-06 | EnableTrading | DISCOVERED status | Blocked |
| I-U-07 | EnableTrading | REVIEWED status | Allowed |
| I-U-08 | CreateVenueInstrument | Valid mapping | Created |
| I-U-09 | ListVenueInstruments | By venue_id | Filtered list |

### 3.2 Handler Tests

| Case | Endpoint | Scenario | Expected |
|------|----------|----------|----------|
| I-H-01 | POST /instruments | Valid | 201 |
| I-H-02 | POST /instruments | Invalid body | 400 |
| I-H-03 | GET /instruments/tradable | Filtered | 200 |
| I-H-04 | PUT /instruments/:id/trading | DISCOVERED | 400 |
| I-H-05 | PUT /instruments/:id/trading | REVIEWED | 200 |
| I-H-06 | POST /venue-instruments | Valid | 201 |
| I-H-07 | GET /venue-instruments | By venue_id | 200 |
| I-H-08 | DELETE /instruments/:id | Valid | 204 |

### 3.3 Integration Tests (existing)

| Case | Function | Scenario | Expected |
|------|----------|----------|----------|
| I-I-01 | Create | Valid | Stored |
| I-I-02 | Create | Duplicate | Error |
| I-I-03 | List | Multiple | Returned |
| I-I-04 | Update | Valid | Updated |
| I-I-05 | Delete | Valid | Removed |

---

## 4. Market Module (`internal/market/`)

### 4.1 Unit Tests (existing)

| Case | Function | Scenario | Expected |
|------|----------|----------|----------|
| M-U-01 | ConnectionManager | Create connection | Stored |
| M-U-02 | ConnectionManager | Update state | Updated |
| M-U-03 | ConnectionManager | IsConnected | Correct |
| M-U-04 | ConnectionManager | GetVenueConnections | Filtered |
| M-U-05 | ConnectionManager | Remove | Deleted |
| M-U-06 | SubscriptionManager | Subscribe | Added |
| M-U-07 | SubscriptionManager | UpdateStatus | Updated |
| M-U-08 | SubscriptionManager | Unsubscribe | Removed |
| M-U-09 | ParseTradeEvent | Valid JSON | Parsed |
| M-U-10 | ParseTickerEvent | Valid JSON | Parsed |
| M-U-11 | ParseFundingEvent | Valid JSON | Parsed |

### 4.2 Missing Unit Tests (new)

| Case | Function | Scenario | Expected |
|------|----------|----------|----------|
| M-U-12 | ProcessRawEvent | Trade event | Trade stored |
| M-U-13 | ProcessRawEvent | Ticker event | Ticker stored |
| M-U-14 | ProcessRawEvent | Funding event | Funding stored |
| M-U-15 | ProcessRawEvent | Duplicate event | Deduplicated |
| M-U-16 | ProcessRawEvent | Invalid event | Error |

### 4.3 Handler Tests (new)

| Case | Endpoint | Scenario | Expected |
|------|----------|----------|----------|
| M-H-01 | GET /market/trades | With data | 200 + trades |
| M-H-02 | GET /market/ticker | With data | 200 + tickers |
| M-H-03 | GET /market/funding | With data | 200 + funding |
| M-H-04 | GET /market/subscriptions | Any | 200 + subscriptions |
| M-H-05 | GET /market/subscribe | WebSocket | Upgrade to WS |

### 4.4 Integration Tests (new)

| Case | Function | Scenario | Expected |
|------|----------|----------|----------|
| M-I-01 | RawRepo.Create | Valid event | Stored |
| M-I-02 | NormalizedRepo | CreateTrade | Stored |
| M-I-03 | NormalizedRepo | CreateTicker | Stored |
| M-I-04 | NormalizedRepo | CreateFunding | Stored |
| M-I-05 | Dedup | SHA-256 hash | Same payload = same hash |

---

## 5. OrderBook Module (`internal/orderbook/`)

### 5.1 Unit Tests (existing — excellent coverage)

| Case | Function | Scenario | Expected |
|------|----------|----------|----------|
| OB-U-01..22 | Engine | Snapshot, delta, health, depth, freshness | All pass |

### 5.2 Missing Tests (new)

| Case | Function | Scenario | Expected |
|------|----------|----------|----------|
| OB-U-23 | handler.GetDepth | HTTP endpoint | 200 + depth |
| OB-U-24 | handler.GetHealth | HTTP endpoint | 200 + health |
| OB-U-25 | handler.GetTradable | HTTP endpoint | 200 + tradable |
| OB-U-26 | handler.Resync | HTTP endpoint | 200 |

---

## 6. UnifiedState Module (`internal/unifiedstate/`)

### 6.1 Unit Tests (existing)

| Case | Function | Scenario | Expected |
|------|----------|----------|----------|
| US-U-01..18 | Engine | Merge, health, depth, snapshot | All pass |

### 6.2 Missing Tests (new)

| Case | Function | Scenario | Expected |
|------|----------|----------|----------|
| US-U-19 | service.UpdateVenueTicker | New ticker | Merged |
| US-U-20 | service.UpdateVenueOrderBook | New depth | Merged |
| US-U-21 | service.UpdateVenueFunding | New funding | Stored |
| US-U-22 | handler.GetInstruments | List | 200 |
| US-U-23 | handler.GetInstrument | By ID | 200 |
| US-U-24 | handler.GetHealth | Health | 200 |

---

## 7. Opportunity Module (`internal/opportunity/`)

### 7.1 Unit Tests (existing)

| Case | Function | Scenario | Expected |
|------|----------|----------|----------|
| OPP-U-01..21 | Calculator/Engine | Price arb, funding arb, basis arb | All pass |

### 7.2 Missing Tests (new)

| Case | Function | Scenario | Expected |
|------|----------|----------|----------|
| OPP-U-22 | service.ScanNow | Trigger scan | Opportunities found |
| OPP-U-23 | service.GetConfig | Return config | Config returned |
| OPP-U-24 | handler.List | List all | 200 |
| OPP-U-25 | handler.Get | By ID | 200 |
| OPP-U-26 | handler.TriggerScan | POST | 200 |
| OPP-U-27 | handler.Delete | By ID | 204 |

---

## 8. Strategy Module (`internal/strategy/`)

### 8.1 Unit Tests

| Case | Function | Scenario | Expected |
|------|----------|----------|----------|
| ST-U-01 | service.Create | Valid | Created |
| ST-U-02 | service.Create | Invalid config | COMMON-902 |
| ST-U-03 | service.GetByID | Found | Returned |
| ST-U-04 | service.GetByID | Not found | COMMON-903 |
| ST-U-05 | service.List | Multiple | All returned |
| ST-U-06 | service.Update | Valid | Updated |
| ST-U-07 | service.Delete | Valid | Deleted |
| ST-U-08 | service.Start | Valid instance | Running |
| ST-U-09 | service.Stop | Running instance | Stopped |
| ST-U-10 | engine.matchesStrategy | Instrument filter | Match/no match |
| ST-U-11 | engine.matchesStrategy | Type filter | Match/no match |
| ST-U-12 | engine.matchesStrategy | Venue filter | Match/no match |
| ST-U-13 | engine.matchesStrategy | Min edge | Above/below threshold |

### 8.2 Handler Tests

| Case | Endpoint | Scenario | Expected |
|------|----------|----------|----------|
| ST-H-01 | GET /strategies | List | 200 |
| ST-H-02 | POST /strategies | Valid | 201 |
| ST-H-03 | GET /strategies/:id | Found | 200 |
| ST-H-04 | PUT /strategies/:id | Valid | 200 |
| ST-H-05 | DELETE /strategies/:id | Valid | 204 |
| ST-H-06 | POST /strategies/:id/start | Valid | 200 |
| ST-H-07 | POST /strategies/:id/stop | Running | 200 |
| ST-H-08 | GET /strategies/:id/decisions | List | 200 |
| ST-H-09 | POST /strategies | No admin | 403 |

---

## 9. Risk Module (`internal/risk/`)

### 9.1 Unit Tests (existing)

| Case | Function | Scenario | Expected |
|------|----------|----------|----------|
| RK-U-01..20 | Calculator/Engine | All risk checks, kill switch | All pass |

### 9.2 Missing Tests (new)

| Case | Function | Scenario | Expected |
|------|----------|----------|----------|
| RK-U-21 | service.CreatePolicy | Valid | Created |
| RK-U-22 | service.ListPolicies | Multiple | Returned |
| RK-U-23 | service.UpdatePolicy | Valid | Updated |
| RK-U-24 | service.DeletePolicy | Valid | Deleted |
| RK-U-25 | service.EnableKillSwitch | Activate | Active |
| RK-U-26 | service.DisableKillSwitch | Deactivate | Inactive |
| RK-U-27 | service.PreTradeCheck | Pass | No blocks |
| RK-U-28 | service.PreTradeCheck | Block by kill switch | Blocked |
| RK-U-29 | handler.ListPolicies | Admin | 200 |
| RK-U-30 | handler.EnableKillSwitch | Admin | 200 |

---

## 10. Execution Module (`internal/execution/`)

### 10.1 Unit Tests (existing)

| Case | Function | Scenario | Expected |
|------|----------|----------|----------|
| EX-U-01..12 | StateMachine | All transitions | All pass |

### 10.2 Missing Tests (new)

| Case | Function | Scenario | Expected |
|------|----------|----------|----------|
| EX-U-13 | policy.Select | Single leg | PARALLEL |
| EX-U-14 | policy.Select | Multi leg | SEQUENTIAL |
| EX-U-15 | service.CreateExecution | Valid | Created |
| EX-U-16 | service.SubmitExecution | Valid | Submitted |
| EX-U-17 | service.CancelExecution | Open order | Canceled |
| EX-U-18 | handler.List | Admin | 200 |
| EX-U-19 | handler.Create | Admin | 201 |
| EX-U-20 | handler.Submit | Admin | 200 |
| EX-U-21 | handler.Cancel | Admin | 200 |

---

## 11. Reconciliation Module (`internal/reconciliation/`)

### 11.1 Unit Tests (existing)

| Case | Function | Scenario | Expected |
|------|----------|----------|----------|
| RC-U-01..5 | Comparator | Balance, position, order, fill, margin | All pass |
| RC-U-06..10 | Engine | Run checks with mock adapter | All pass |

### 11.2 Missing Tests (new)

| Case | Function | Scenario | Expected |
|------|----------|----------|----------|
| RC-U-11 | service.StartReconciliation | Valid | Run created |
| RC-U-12 | service.GetRun | Found | Run returned |
| RC-U-13 | service.ListRuns | Multiple | All returned |
| RC-U-14 | service.ListItems | By run_id | Items returned |
| RC-U-15 | handler.CreateRun | Admin | 201 |
| RC-U-16 | handler.ListRuns | Admin | 200 |
| RC-U-17 | handler.GetRun | Admin | 200 |

---

## 12. Storage Module (`internal/storage/`)

### 12.1 Unit Tests (existing)

| Case | Function | Scenario | Expected |
|------|----------|----------|----------|
| S-U-01..10 | Models | All model structures | Valid |

### 12.2 Missing Tests (new)

| Case | Function | Scenario | Expected |
|------|----------|----------|----------|
| S-U-11 | oppRepo.Create | Store opportunity | Stored |
| S-U-12 | oppRepo.GetByID | Found | Returned |
| S-U-13 | decisionRepo.Create | Store decision | Stored |
| S-U-14 | auditRepo.Create | Store event | Stored |
| S-U-15 | retention.Cleanup | Delete old data | Cleaned |
| S-U-16 | handler.ListOpportunities | Query | 200 |
| S-U-17 | handler.ListDecisions | Query | 200 |

---

## 13. Funding Arbitrage Module (`internal/fundingarbitrage/`)

### 13.1 Unit Tests (new — ZERO existing)

| Case | Function | Scenario | Expected |
|------|----------|----------|----------|
| FA-U-01 | cache.Get | Hit | Value returned |
| FA-U-02 | cache.Get | Miss | nil |
| FA-U-03 | cache.Set | Store value | Cached |
| FA-U-04 | cache.Invalidate | Clear key | Removed |
| FA-U-05 | cache.GetStats | Any | Stats returned |
| FA-U-06 | model.APR | Calculation | Correct APR |
| FA-U-07 | model.APY | Calculation | Correct APY |
| FA-U-08 | HyperliquidAdapter.FetchAllFunding | Valid metaAndAssetCtxs fixture (object envelope `{universe:[...]}` + ctx array) | Perps parsed with funding/mark/index/OI |
| FA-U-09 | HyperliquidAdapter.FetchAllFunding | Delisted market in universe | Skipped |
| FA-U-10 | HyperliquidAdapter.FetchAllFunding | Universe/ctx length mismatch | Skipped extras, no crash |
| FA-U-11 | HyperliquidAdapter.FetchAllFunding | HTTP error / bad JSON | Error returned |
| FA-U-12 | HyperliquidAdapter.FetchAllFunding | Live-shape excerpt (marginTables/collateralToken siblings); missing `universe` key | Parses OK; missing universe → error |

### 13.2 Handler Tests (new)

| Case | Endpoint | Scenario | Expected |
|------|----------|----------|----------|
| FA-H-01 | GET /public/venues | Any | 200 + venues |
| FA-H-02 | GET /funding/arbitrage | Any | 200 + arbitrage data |

---

## 14. Collector Module (`internal/collector/`)

### 14.1 Unit Tests (new — ZERO existing)

| Case | Function | Scenario | Expected |
|------|----------|----------|----------|
| C-U-01 | NormalizeSymbol | "BTCUSDT" | base=BTC, quote=USDT |
| C-U-02 | NormalizeSymbol | "BTC-USD" | base=BTC, quote=USD |
| C-U-03 | CalculateAnnualizedRate | 0.01 daily | ~365% APR |
| C-U-04 | NormalizeBaseAsset | "BTC" (hyperliquid coin, no suffix) | base=BTC |
| C-U-05 | NormalizeQuoteAsset | "BTC" (hyperliquid, no suffix) | quote=USD default |

---

## 15. Cross-Module E2E Tests

| Case | Flow | Steps | Expected |
|------|------|-------|----------|
| E2E-01 | Full lifecycle | venue → instrument → market data → opportunity → strategy | Opportunity detected |
| E2E-02 | Risk blocks execution | enable kill switch → try execute → blocked | Execution blocked |
| E2E-03 | Reconciliation | create positions → reconcile → detect mismatch | Mismatch found |
| E2E-04 | Data retention | store old data → cleanup → verify removed | Cleaned |
| E2E-05 | Multi-tenant isolation | tenant A data → tenant B query → empty | Isolated |

---

## 16. Security & RBAC

| Case | Scenario | Expected |
|------|----------|----------|
| SEC-01 | Non-admin access admin endpoint | 403 |
| SEC-02 | Unauthenticated access to protected endpoint | 401/403 |
| SEC-03 | JWT tamper | Reject |
| SEC-04 | Rate limit trigger | 429 |
| SEC-05 | SQL injection in query params | No effect |
| SEC-06 | XSS in venue/instrument name | Sanitized |

---

## 17. Wallet Dashboard Feature -- RETIRED at cutover

The legacy scanner (`internal/wallet/`, `internal/walletgroup/`, routes `/wallets` + `/groups`, tables `tracked_wallets`, `wallet_fills`, `wallet_metric_snapshots`, `user_wallet_groups`, `group_wallet_members`, `user_wallet_tags`, `user_wallet_watchlist`) was removed after FE migrated to the v1.1 API (migration 000025 drops the tables). See section 19 for the replacement suite. The section 17 cases are preserved in git history and no longer run.

---

## 18. Swagger Sync (`docs/swagger.json`, `internal/httpserver/swagger_sync_test.go`)

Swagger must mirror the live system: every `@Router` annotation has a path,
every path has an annotation, and exactly the JWT-protected endpoints carry
`@Security BearerAuth`. The secured list in `expectedSecured` mirrors
`routes.go` — adding/removing a JWT route requires updating it (the test
fails on drift in either direction).

| Case | Function | Scenario | Expected |
|------|----------|----------|----------|
| DOCS-S-01 | TestDocs_AnnotationsMatchSwaggerPaths | Set-diff `@Router` ↔ `swagger.json` paths (all `internal/` + `cmd/`) | Both directions empty; counts equal (currently 97) |
| DOCS-S-02 | TestDocs_SecurityMatchesRoutes | Set-diff secured-in-swagger ↔ `expectedSecured` (48 entries: auth×6, venues×5, strategies×8, risk×9, executions×7, reconciliation×4, trader-groups×9) | Protected without `@Security` → fail; public marked secured → fail; `securityDefinitions.BearerAuth` present |

Swagger-only endpoint groups (documented, no code change): venues/groups
gained `@Security` + 401/403 rows; opportunity (6, public), strategies (8),
risk (9), executions (7), reconciliation (4) gained full annotations with
tags `opportunities/strategies/risk/executions/reconciliation`.

---

## 19. Trader Scanner v1.1 (`internal/trader/`, `internal/tradergroup/`)

Rewrite of the wallet scanner per `Hyperliquid_Trader_Scanner_BE_Spec_v1.1.docx`
(spec v1.0 + approved deltas D1–D9). Contract: `POST /api/v1/traders/search`,
`GET /api/v1/traders/{wallet}?venue=`, `/api/v1/trader-groups/...` (generic
routes + `venue` param; multi-venue ready, V1 ships `venue=hyperliquid` only).
Dual-run with §17 (`/wallets`, `/groups`) until the cutover release drops them.

Scope decisions: V1 = leaderboard discovery + incremental sync + metrics +
scanner/detail/groups (WS discovery, portfolio/equity/drawdown → V1.1).
Schema: new tables keyed by `(venue, address)` in parallel; old tables dropped
at cutover. ROI = leaderboard window passthrough where the window matches,
else the documented computed formula (never silent substitution). Display PnL
follows the same passthrough; `realized_pnl` column (migration 000024) always
stores the computed fills-based sum for reconciliation.

Contract spike (verified live 2026-10-01): `GET
https://stats-data.hyperliquid.xyz/Mainnet/leaderboard` → 200,
`{"leaderboardRows": [...]}` (46,991 rows, no auth, single dump, ~39MB).
Row: `ethAddress`, `accountValue` (string), `displayName` (nullable), `prize`,
`windowPerformances: [[window, {pnl, roi, vlm}], ...]` with windows
`day/week/month/allTime`; ROI is a fraction (×100 for pct). Pinned fixture:
`internal/hyperliquid/testdata/leaderboard_sample.json` (5 rows, BE-031).

Spec fixtures A–L (trade reconstruction + scale): A simple long win; B short
loss; C partial entries+exits; D long→short flip; E breakeven; F multi-coin
interleave; G duplicate events/fills; H same-timestamp different tid; I
fees+funding; J deposits/withdrawals; K leaderboard+WS merge source=both;
L 100k+ synthetic scanner dataset (perf, V1.1).

Test hermeticity: integration/e2e suites use dedicated venues
(`trader-test-venue`, `trader-e2e-venue`) so counts stay deterministic while
the live server's workers write `venue=hyperliquid` rows into the shared DB.

### 19.1 Unit

| Case | Function | Input | Expected |
|------|----------|-------|----------|
| DISC-U-01 | Leaderboard parse | Pinned 5-row sample | N rows, lowercase addresses, roi×100, month window mapped |
| DISC-U-02 | Missing window entry | Row without month performance | Row kept, month stats null, no crash |
| DISC-U-03 | Malformed address (BE-006) | `ethAddress: "0xZZZ"` | Row skipped + counted, no DB write |
| DISC-U-04 | Payload guard | 39MB+ / truncated JSON | Size-capped reader, no OOM, clean error |
| SYNC-U-01 | Cursor advance (BE-011) | Same fills window processed twice | Second run no-op; cursor monotonic |
| TRD-U-01 | Reconstruction (BE-012/013) | Fixture C: 3 partial entries, 2 partial exits | One completed trade; size/PnL aggregate |
| TRD-U-02 | Flip (BE-014) | Fixture D: long→flat→short | Two cycles, no mixed-direction trade |
| TRD-U-03 | Breakeven (BE-015) | Fixture E: net PnL = 0 | Breakeven +1; win/loss unchanged |
| TRD-U-04 | Interleave (BE-040) | Fixture F + midnight-crossing close | Per-coin cycles; UTC date attribution |
| TRD-U-05 | Duplicates (BE-011) | Fixture G/H: dup fills, same-ts tids | Each fill contributes exactly once |
| TRD-U-06 | Fees/funding (BE-009-spec) | Fixture I: fees + funding activity | Net = closedPnl − fees; funding tracked separately, never decides win/loss |
| MET-U-01 | Win rate (BE-016) | 2 wins, 1 loss, 1 breakeven | 66.67%; breakeven excluded |
| MET-U-02 | Profit factor (BE-017/018) | Gross 300/−100; then loss 0 | 3.0; null (never Infinity/NaN) |
| MET-U-03 | Long/short WR (BE-019) | Long 3/4, short 1/2 | 75% / 50% |
| MET-U-04 | Drawdown formula (BE-022) | Equity 100→120→90 | 25% from peak (V1.1 job; formula tested V1) |
| MET-U-05 | Daily idempotency (BE-020) | Recompute same day twice | Identical row after second run |
| MET-U-06 | ROI/PnL rule | LB window match vs no match | Passthrough (ROI×100, PnL) + realized_pnl always computed + version bump |
| CUR-U-01 | Cursor codec (BE-026) | Encode → decode roundtrip; tampered cursor | Roundtrip stable; tampered → INVALID_FILTER |
| VAL-U-01 | Filter validation (BE-027/039) | roi_min>roi_max; pnl=0; volume=−1 | 4xx INVALID_FILTER for range/negative; 0 valid where allowed |
| OBS-U-01 | Health counters | Fake discover/sync runs (success, error, 429) | done/failed/fetch/rate-limit/latency tracked; streak resets on success |

### 19.2 Handler

| Case | Function | Scenario | Expected |
|------|----------|----------|----------|
| SRCH-H-01 | POST /traders/search (BE-024) | period 30D + pnl_min + roi_min | 200; only AND-matching rows |
| SRCH-H-02 | Sorting (BE-025) | sort_by=pnl desc then asc | Server order flips; stable tiebreak |
| SRCH-H-03 | Pagination (BE-026) | 3 pages over fixture L-small | No duplicates/skips within stable query |
| SRCH-H-04 | Invalid filter (BE-027) | roi_min>roi_max | 4xx INVALID_FILTER, no query executed |
| SRCH-H-05 | Venue (D7) | Missing venue → default; unknown venue | Default hyperliquid; unknown → 4xx |
| SRCH-H-06 | Freshness (BE-037) | Wallet older than stale threshold | 200 with data_status=stale per row |
| TRD-H-01 | GET /traders/{wallet} | Known + unknown address | 200 header+metrics+as_of; unknown → 404 |
| TRD-H-02 | No inline sync (BE-038) | Detail for inactive wallet, HL mocked | Zero upstream calls; sync priority queued |
| GRP-H-01 | trader-groups CRUD | Create/rename/delete | 201/200/204; delete keeps registry rows (BE-030) |
| GRP-H-02 | Members (BE-029) | POST same wallet twice | Second idempotent, no duplicate row |
| GRP-H-03 | Isolation (BE-028) | User A touches B's group/member paths | 404/403, no leak |
| AUTH-H-01 | Auth (BE-034) | Scanner without token; groups without token | Scanner 200; groups 401/403 |

### 19.3 Integration (`//go:build integration`)

| Case | Function | Scenario | Expected |
|------|----------|----------|----------|
| DISC-I-01 | Leaderboard sync ×2 (BE-001/002) | Same payload twice | One row per wallet; display_name updated; first_seen_at unchanged |
| DISC-I-02 | Source merge (BE-005) | Leaderboard row + WS-sourced row (simulated insert) | discovery_source=both |
| SYNC-I-01 | Full pipeline | Seed registry → sync fills → daily → period → search | Search returns the row with computed metrics |
| SYNC-I-02 | Crash recovery (BE-033) | Commit data, die before cursor update | Reprocess, no double-count |
| SYNC-I-03 | Backoff (BE-010) | Upstream 429s | Retries respect backoff; queue drains, no hot-loop |
| SYNC-I-04 | Timeout (BE-032) | Info API timeout | Recoverable sync state, retry scheduled |
| SYNC-I-05 | Schema drift (BE-031) | Leaderboard missing expected field | Job fails safe + alert; old data intact |
| OBS-I-01 | Stale count | Fresh + old + error period rows | Counts old-vintage + error rows, excludes fresh |

### 19.4 E2E (`//go:build e2e`)

| Case | Function | Scenario | Expected |
|------|----------|----------|----------|
| E2E-T-01 | Discover→groups flow | Fake leaderboard (20 wallets) → discover → sync (mock fills) → search → detail → group add/remove | Counts consistent end-to-end; member aliases persist |
| E2E-T-02 | Isolation | Two users, trader-groups | Cross-user access rejected (BE-028) |

### 19.8 Group members v2 (BE-1 metrics, BE-2 PATCH)

| Case | Function | Scenario | Expected |
|------|----------|----------|----------|
| GRP-M-01 | UpdateMemberInput.Validate | Absent/present/empty/caps/bad venue/bad address | Tri-state correct; caps 100/500 enforced; 400s |
| GRP-M-02 | PATCH handler | Shape {updated}, anonymous 403, over-cap 400, foreign 403 | Mappings correct; store untouched on validation fail |
| GRP-M-03 | UpdateMembers repo | Set + clear-to-NULL + idempotent rerun + absent no-op + unknown venue/wallet + isolation | updated counts exact; 400/404/403 per rule |
| GRP-M-04 | ListMembers metrics | Member with/without 30D row; bad/empty period | Metrics present/null respectively; 90D → INVALID_FILTER; empty → 30D |

### 19.9 Numbered pagination (CONTRACT.md 2026-10-06, BE worker)

`POST /api/v1/traders/search` gains an additive numbered-pagination path:
`page` (`*int`, `>= 1`; absent = legacy keyset path) switches to
`LIMIT/OFFSET` (`offset = (page-1)*limit`) plus a `COUNT(*)` with identical
filters for `total` / `total_pages = ceil(total / limit)`. When `page` is
present the `cursor` in the same body is ignored (precedence: page wins).
`limit` stays `1..100`. `page > total_pages` (and `total > 0`) returns empty
data with `has_more=false`. Cursor path is behavior-identical (same rows,
same `cursor`/`has_more`) and additionally returns `total`/`total_pages`.
`page` is intentionally NOT part of the cursor `Fingerprint`: the offset path
is stateless (no sealed cursor to validate), so there is nothing to
invalidate; the cursor codec/fingerprint for the keyset path is untouched.

| Case | Function | Input | Expected |
|------|----------|-------|----------|
| PG-U-01 | SearchRequest.Normalize/EffectivePage | `{}` (page absent) | `Page` stays nil (cursor path); `EffectivePage()` = 1 |
| PG-U-02 | SearchRequest.Validate | `page: 0` | INVALID_FILTER 400 |
| PG-U-03 | SearchRequest.Validate | `page: -1` | INVALID_FILTER 400 |
| PG-U-04 | SearchRequest.Validate | `page: 1`, `limit: 20` | valid |
| PG-U-05 | TotalPages | total/limit = 0/20, 41/20, 40/20, 5/10 | 0, 3, 2, 1 |
| PG-U-06 | buildSearchOffsetQuery | filters + sort pnl desc + limit 2 + offset 2 | Same WHERE fragments as keyset query; `ORDER BY p.pnl DESC NULLS LAST, p.wallet_address ASC`; `LIMIT $n OFFSET $m` |
| PG-U-07 | buildSearchCountQuery | same filters | `SELECT COUNT(*)` with identical WHERE (no ORDER/LIMIT/cursor predicates) |
| PG-U-08 | Fingerprint | same filters, page 1 vs page 2 | Equal (page excluded by design; offset path stateless) |

| Case | Endpoint | Scenario | Expected |
|------|----------|----------|----------|
| PG-H-01 | POST /traders/search | `{"page":2,"limit":2}` (mock rows + total) | 200; `meta.page=2`, `meta.total`, `meta.total_pages`; data = that page only |
| PG-H-02 | POST /traders/search | `{"page":0}` via service INVALID_FILTER | 400 INVALID_FILTER |
| PG-H-03 | POST /traders/search | `{"page":"abc"}` (non-int JSON) | 400 INVALID_FILTER (bind-failure shape) |
| PG-H-04 | POST /traders/search | `{"page":1,"cursor":"forged.cursor"}` (mock ok) | 200 (cursor ignored, offset wins) |

| Case | Function | Scenario | Expected |
|------|----------|----------|----------|
| PG-I-01 | Service.Search offset walk | 5-row fixture, limit 2, pages 1→2→3 | Exact coverage, no dup/missing; total=5, totalPages=3; has_more true/true/false |
| PG-I-02 | Service.Search beyond total | page 4, total 5, limit 2 | Empty data, has_more=false, total_pages=3 |
| PG-I-03 | Service.Search empty set | filter matching 0 rows, page 1 | Empty data, total=0, totalPages=0, has_more=false |
| PG-I-04 | Service.Search partial tail | page 2, limit 10, total 5 | 5 rows on page 1; page 2 empty (limit > remaining) |
| PG-I-05 | Service.Search page+cursor | page 1 + forged cursor | 200 offset results (no INVALID_FILTER) |
| PG-I-06 | Service.Search invalid pages | page 0 / -1 | INVALID_FILTER, no query executed |
| PG-I-07 | Service.Search stable sort | static fixture, sort pnl desc, limit 2 | Pages 1-2 disjoint and union = top-4; wallet tiebreak stable |
| PG-I-08 | Service.Search repeatability | same page twice on static data | Identical rows (stateless determinism for FE retry) |
| PG-I-09 | EXPLAIN COUNT | realistic filters (venue+period+pnl_min) | Index (no seq scan on period metrics); plan + timings in BE report |

| Case | Flow | Steps | Expected |
|------|------|-------|----------|
| PG-E-01 | Numbered walk over HTTP | seed 3 rows → page 1 (limit 2) → page 2 → page 3 | 2 rows + total_pages=2 → 1 row → empty + has_more=false; `meta.page` echoes |
| PG-E-02 | Stateless deep link | page 2 as first request (no prior cursor) | Correct rows without any cursor state |

Out of BE scope (FE owns per CONTRACT §3): rapid page-click latest-wins,
`?page=` URL state, `page > totalPages` auto-fall to last page, 500-on-page-2
retry UX. BE contribution: every page request is independent/stateless, so a
retry of the same page is deterministic (PG-I-08) and total-shrink clamps via
PG-I-02.

### 19.7 Retention (DB size control)

Retention tests run against the shared test DB alongside live workers:
assert only rows the test seeded (address-scoped), never global counts —
live backfills legitimately write historical days. Each suite uses its own
test venue (`trader-test-venue`, `tradergroup-test-venue`, `trader-e2e-venue`)
since packages run in parallel. Register `t.Cleanup(pool.Close)` BEFORE any
data cleanup: `defer pool.Close()` runs first and deletes silently fail on the
closed pool, leaking rows.

| Case | Function | Scenario | Expected |
|------|----------|----------|----------|
| RET-I-01 | Buffer purge | Staged fills older than retention + recent | Old gone after sync, recent kept |
| RET-I-02 | Daily/equity retention | Rows older than window + recent | Old deleted, recent kept (400d default) |
| RET-I-03 | Dead-wallet prune | Dead pruned; traded/grouped/never-synced kept; re-entry | Seeded dead gone; others stay; address re-enters |
| RET-I-04 | Full cleanup partial | Missing orderbook_* tables in this DB | Continues past them; trader counts present; error joined |

### 19.5 Deferred (still)

90D period (not in spec enum).

### 19.6 V1.1 WS trade discovery

Spike-verified contract: `wss://api.hyperliquid.xyz/ws`, subscribe
`{"method":"subscribe","subscription":{"type":"trades","coin":"BTC"}}`;
frames `{"channel":"trades","data":[{..., "time":ms, "users":[buyer, seller]}]}`.
Coin universe from `POST /info {"type":"meta"}` (234 perps observed).

| Case | Function | Input | Expected |
|------|----------|-------|----------|
| WS-U-01 | Trade frame parse | trades frame + subscriptionResponse + malformed JSON + unknown channel | Buyer/seller extracted; rest ignored/skipped, no crash |
| WS-U-02 | Address validation | users[] with malformed entry | Malformed skipped, valid kept |
| WS-U-03 | Subscription cap (BE-009) | 300 coins, cap 250 | Refused over cap, counted, alert field set |
| WS-U-04 | Resubscribe set | Reconnect with 3 coins | All 3 resubscribed, no duplicates |
| WS-U-05 | Batch dedupe | Same address ×50 events, mixed times | One upsert with max trade time |
| WS-U-06 | Harvest validation | Empty/malformed batch entries | Skipped safely, reported in counts |
| EQ-U-01 | Portfolio parse | Pinned upstream shape + bad entries | Per-window points; broken entries skipped loudly |
| EQ-U-02 | Equity aggregation (BE-021) | Intraday points across 2 days | start/end/peak/return per UTC day |
| EQ-U-03 | Drawdown formula (BE-022) | 100→120→90; monotonic; empty | 25%; 0; nil |

| Case | Function | Scenario | Expected |
|------|----------|----------|----------|
| WS-I-01 | Local WS server → harvest (BE-003) | Server emits trades with unseen buyer/seller | Both registry rows, source=ws_trade, last_trade_at set |
| WS-I-02 | Duplicate delivery (BE-004) | Same frame twice | One row per wallet; idempotent |
| WS-I-03 | Source merge (BE-005) | Leaderboard-seeded wallet seen on WS | source=both, first_seen_at unchanged |
| WS-I-04 | Reconnect (BE-007) | Kill server mid-stream, restart | Manager reconnects + resubscribes, harvesting resumes |
| WS-I-05 | Dynamic coin (BE-008) | Meta gains a coin, refresh | New coin subscribed without restart |
| WS-I-06 | Burst (BE-036) | 20k events in seconds | Bounded queue, drops counted, all unique wallets land |
| EQ-I-01 | Equity sync (BE-021) | Fake portfolio over 3 days | Equity rows stored exactly; last_portfolio_sync_at set |
| EQ-I-02 | Drawdown wiring (BE-022) | Same run, 30D recalc | max_drawdown_pct = 25% from the curve |
| EQ-I-03 | Wide numerics (live 22003) | Dust-to-millions return + huge PF | Upsert + roundtrip, no overflow |

| Case | Function | Scenario | Expected |
|------|----------|----------|----------|
| E2E-T-03 | WS → detail | Fake WS emits trade for unknown wallet | GET /traders/{wallet} 200, source=ws_trade, metrics null |
| E2E-T-04 | Member patch+metrics | Add w/ alias → GET (metrics present/null) → PATCH set+clear → rerun no-op → period=7D null | {updated} exact; alias set; note NULL; 7D null |

### 19.10 Trader detail: positions + activity (DETAIL-PLAN.md A1–A9)

Scope (M2): positions sync covers watched wallets (`ActivityHub.WatchSet()`,
i.e. wallets with a live WS subscriber = currently-viewed detail pages) plus
recently-traded wallets on their normal sync pass; all other wallets use a
long interval + jitter (see POS-S-01). Worst-case HL `clearinghouseState`
rate = watched + due-hot wallets per 30s tick (bounded by the sync worker
pool, sharing the venue pacer with fills/portfolio — no per-wallet timer).
Worst-case formula: `|WatchSet| x 1 clearinghouseState per 30s + cold-pass due`
(e.g. 50 watched wallets → 50 calls/30s ≈ 1.7 rps, plus at most the cold wallets due that tick).

Status (M5): `last_positions_sync_at IS NULL` (never synced) reports
`data_status=syncing`, never `stale`.

Upstream auth (M8, verified 2026-10-07, report only): `POST https://api.hyperliquid.xyz/info {"type":"webData2","user":"0x0000...0000"}` returns 200 with `clearinghouseState` unauthenticated —
conclusion: `webData2` is PUBLIC; polling architecture unchanged.

Schema doc: `DATABASE_DESIGN.md` §10.3a updated on disk at `arbitrage-platform-docs/DATABASE_DESIGN.md` lines 722-790 (that dir is gitignored per `.gitignore:20`, so it cannot be committed).

| Case | Function | Input | Expected |
|------|----------|-------|----------|
| POS-U-01 | HLPositionAdapter mapping | `szi` +/−, decimal strings, zero/unparseable `szi` | + → LONG, − → SHORT, size=\|szi\|; bad rows skipped; mark=‖value‖/size; summary parsed |
| POS-U-02 | parseOpt/DecimalString | `"12.5"`, `20` (number), `""`, `null` | floats / nil, no crash |
| ACT-U-01 | WSActivityService.Submit | fills with buyer/seller watched/unwatched, self-trade | watched buyer → BUY, watched seller → SELL, self-trade once as BUY; unwatched skipped |
| ACT-U-02 | ActivityHub publish | full subscriber buffer (bufSize 1) | non-blocking drop, no goroutine block |
| ACT-U-03 | CompletedTradeRow.NetPnl | pnl=1500, fees=30 | 1470 |
| POS-S-01 | Position scope (M2) | watched vs unwatched wallets, cold interval + jitter | watched → every 30s; unwatched → every 24h + deterministic hash jitter; first sync always; nil fetcher never |

| Case | Endpoint | Scenario | Expected |
|------|----------|----------|----------|
| POS-H-01 | GET /traders/{wallet}/positions | synced wallet (mock service) | 200; summary + per-coin rows; `data_status=ready`; `positions` never null |
| POS-H-02 | GET /traders/{wallet}/positions | unknown wallet | 404 (Detail error path) |
| POS-H-03 | GET /traders/{wallet}/positions | invalid address | 400 INVALID_FILTER/COMMON-902 |
| POS-H-04 | GET /traders/{wallet}/positions | never synced (`last_positions_sync_at` NULL) | 200 with `data_status=syncing` (M5), summary null, positions `[]` |
| ACT-H-01 | GET /traders/{wallet}/activity | default (no limit/cursor) | 200; limit=20; rows newest-first with `net_pnl=pnl-fees`; `has_more` + `next_cursor` |
| ACT-H-02 | GET /traders/{wallet}/activity | `limit=0` / `limit=101` | 400 INVALID_FILTER |
| ACT-H-03 | GET /traders/{wallet}/activity | tampered cursor | 400 INVALID_FILTER |
| ACT-H-04 | GET /traders/{wallet}/activity | unknown wallet | 404 |
| WS-H-01 | GET /traders/ws (M3) | httptest + real WS dial: connect → `subscribed` → Publish → receive | client gets `{type:subscribed}` then `{type:activity}` with fill fields |
| WS-H-02 | GET /traders/ws | `{"type":"ping"}` | `{type:pong}` reply |
| WS-H-03 | GET /traders/ws | close conn | unsubscribed (WatchSet shrinks); invalid wallet → 400, no upgrade |

| Case | Function | Scenario | Expected |
|------|----------|----------|----------|
| POS-I-01 | ReplacePositions idempotent | same snapshot twice | identical rows (summary + positions) after second run |
| POS-I-02 | Coin removal | snap A {BTC,ETH} → snap B {BTC} | ETH row deleted, BTC updated |
| POS-I-03 | FK cascade | DELETE trader_registry row | positions + summary + trades rows gone |
| POS-I-04 | ReplaceTradesForDay deterministic | same day recomputed twice; then empty list | identical rows; empty recompute deletes stale day rows |
| ACT-I-01 | ListTrades keyset | 3 closed trades, limit 2 → page 2 via cursor | page 1 = newest 2 + has_more; page 2 = last 1, no dup/skip; order (closed_at DESC, market ASC, opened_at ASC) |
| ACT-I-02 | PruneTrades | rows older/newer than 15d cutoff | only old rows deleted; count exact |
| SYNC-I-07 | syncPositions failure | fetcher errors | wallet pass still succeeds; last snapshot kept; `last_positions_sync_at` untouched |
| SYNC-I-08 | SyncWallet persists trades | fake fills closing a cycle | `trader_trades` rows written; `last_positions_sync_at` set on success |

| Case | Flow | Steps | Expected |
|------|------|-------|----------|
| E2E-T-05 | Positions flow (M7) | seed registry + sync-state + positions/summary → GET positions | 200 summary + rows + `data_status`; unknown wallet 404 |
| E2E-T-06 | Activity flow (M7) | seed 3 closed trades → GET activity limit=2 → follow `next_cursor` | page 1 (2 rows, has_more=true) → page 2 (1 row); `net_pnl` correct; bad cursor 400 |

### 19.11 Wallet tabs (WALLET-TABS-CONTRACT.md v1 frozen 2026-10-08)

Scope: WS1 TRADES (migration 000030 + `ReconstructTrades` entry/exit/size +
activity sort/filter/counts + keyset cursor by sort), WS2 BALANCES
(`FetchSpotState` + `withdrawable`/`crossMarginSummary`), WS3 ORDERS,
WS4 FILLS (`FetchUserFills` + tid paging), WS5 TRANSFERS
(`FetchLedgerUpdates` + type enum), WS6 PERFORMANCE (Detail metrics +
equity). Shared: 15s TTL cache keyed by (type,wallet,params), graceful
degradation on HL error. Endpoints: `GET /traders/{wallet}/{balances,
fills,orders,transfers,performance}` (new) + `GET .../activity` extended
(sort/filter/counts/entry-exit) + `GET .../positions` sortable.

#### Unit

| Case | Function | Input | Expected |
|------|----------|-------|----------|
| WT-U-01 | Reconstruct entry/exit | Fixture C partials: 3 opens @100/102/104, 2 closes @110/112 | `entry_price`=wavg opens, `exit_price`=wavg closes, `size`=Σ open qty |
| WT-U-02 | Flip split | Fixture D long→short flip @px | Closing leg `|before|` exits old cycle, `|after|` opens new cycle at same px |
| WT-U-03 | Truncated NULL | Cycle with no opening leg in window (fills aged out) | `entry_price`=NULL; `exit_price` computed; pre-000030 rows keep NULL |
| WT-U-04 | Breakeven entry/exit | Fixture E net 0 with entry 100 exit 100 | Breakeven counted; entry/exit present, `net_pnl`=0 |
| WT-U-05 | sortActivityRows volume | 3 rows vol 1k/5k/30k, dir asc+desc | asc 1k→30k, desc 30k→1k; tiebreak (closed_at, market, opened_at) stable |
| WT-U-06 | sort nulls last | entry_price {10, NULL, 5}, dir asc and desc | NULL last in both dirs; non-null ordered |
| WT-U-07 | filterActivityRows | 2 LONG win, 1 SHORT loss, 1 breakeven | `result=win`→2, `side=long`→2, `win+long`→2; breakeven in neither win nor loss |
| WT-U-08 | activityCounts stable | Same 4 rows | `{win:2, loss:1, long:2, short:1, total:4}` independent of pagination/filters |
| WT-U-09 | Activity cursor V2 | Encode→decode roundtrip; tampered sig; wrong fingerprint | Roundtrip stable over (sortKey, closed_at, market, opened_at); tampered/wrong-fp → INVALID_FILTER |
| WT-U-10 | Balances parse | `clearinghouseState` + `spotClearinghouseState` fixture (withdrawable, crossMarginSummary, balances) | Perp fields mapped; `asset_positions_value`=Σ|value|; spot rows upper-cased coin; bad rows skipped |
| WT-U-11 | Balances partial | Perp OK + spot error (and vice versa) | Failing side null; `data_status=error`; both fail → stale cache or empty + error |
| WT-U-12 | mapUserFills | Mixed sides/sz/px/time + bad rows (sz≤0, px<0, time≤0, side unknown) | Bad skipped; valid newest-first (time DESC, tid DESC) |
| WT-U-13 | mapOpenOrders/mapHistorical | Open + historical fixtures (triggerPx, tpsl, bad side/coin/sz/time) | Bad skipped; open lacks `order_status`; historical has `order_status`+`status_timestamp` |
| WT-U-14 | mapLedgerUpdates | Deltas: deposit/withdraw/send/subAccountTransfer/unknown-type + bad time/hash | Unknown→`other`; `is_deposit` true/false/nil per rule; newest-first (time DESC, hash ASC) |
| WT-U-15 | Performance period | `1D/7D/30D/ALL` vs `90D`/empty | Valid pass; empty→30D; `90D` → INVALID_FILTER; lookback 1/7/30/365d |
| WT-U-16 | OnDemandCache TTL | Set → get fresh → expire → stale retained | Fresh hit returns value; expired returns stale+`fresh=false` for degradation |

#### Handler

| Case | Endpoint | Scenario | Expected |
|------|----------|----------|----------|
| WT-H-01 | GET /balances | Known wallet, mock service OK | 200; `perp`+`spot` present or null-safe; `data_status` string |
| WT-H-02 | GET /balances | Unknown wallet | 404 COMMON-903 (Detail error path) |
| WT-H-03 | GET /balances | Invalid address | 400 INVALID_FILTER/COMMON-902 |
| WT-H-04 | GET /fills | Default (no limit/cursor) | 200; limit=100; rows newest-first; `has_more`+`next_cursor` |
| WT-H-05 | GET /fills | `limit=0/201/abc`, tampered cursor | 400 INVALID_FILTER |
| WT-H-06 | GET /orders | `status=open` vs `historical` | 200; open rows lack status fields, historical has them; `limit` 1..2000 else 400 |
| WT-H-07 | GET /orders | `status=bad` | 400 INVALID_FILTER |
| WT-H-08 | GET /transfers | Default days=30 limit=200 | 200; rows newest-first; `has_more`+`next_cursor` |
| WT-H-09 | GET /transfers | `days=0/181`, `limit=0/501`, tampered cursor | 400 INVALID_FILTER |
| WT-H-10 | GET /performance | `period=30D` (and 1D/7D/ALL) | 200; `period` echoes; `metrics`+`equity[]` |
| WT-H-11 | GET /performance | `period=90D` / unknown wallet / invalid address | 400 / 404 / 400 respectively |
| WT-H-12 | GET /activity sort/filter | `sort=volume&dir=asc`, `result=win&side=long` | 200; server order monotonic; only LONG winners; `counts` unchanged |

#### Integration (`//go:build integration`)

| Case | Function | Scenario | Expected |
|------|----------|----------|----------|
| WT-I-01 | Migration 000030 up | Apply on clean DB | `entry_price/exit_price/size` NULLABLE numeric(30,12) on `trader_trades` |
| WT-I-02 | Migration 000030 down | Rollback | Three columns dropped; 000029 shape intact |
| WT-I-03 | ReplaceTradesForDay entry/exit | Seed fills closing a cycle with partials | `trader_trades` row carries entry/exit/size; recompute deterministic |
| WT-I-04 | ListTrades sort/filter/counts | 4-row fixture (LONG win ×2, SHORT loss ×1, breakeven ×1) | `sort=volume asc` ordered; `result=win&side=long` filters rows but `counts` stable |
| WT-I-05 | Performance wiring | Seed 30D metrics + 3 equity_daily rows | `Performance` returns metrics non-null + 3-point equity curve; unknown period → INVALID_FILTER |
| WT-I-06 | On-demand nil-client | Service without `WithOnDemand` | Balances `data_status=error`; fills/orders/transfers empty rows (never null, never 500) |

#### E2E (`//go:build e2e`)

| Case | Flow | Steps | Expected |
|------|------|-------|----------|
| E2E-T-07 | Wallet-tabs on-demand flow | `traderRandAddr` registry → GET balances/fills/orders(open+historical)/transfers/performance → unknown wallet → invalid params | 200 degraded/empty (nil on-demand) + `data_status`/`rows` shapes; unknown → 404; `limit=0`/`days=0`/`status=bad`/`period=90D` → 400 |
| E2E-T-08 | Activity sort/filter/counts | Seed 4 trades (LONG win ×2, SHORT loss ×1, breakeven ×1) → `sort=volume&dir=asc` → `result=win&side=long` → cursor walk by sort | Volume monotonic; filter only LONG winners; `counts={win:2,loss:1,long:2,short:1,total:4}` stable across filters/pages; no dup/skip |

Env deviations (M6): no cgo/gcc → gates run WITHOUT `-race` (`go test
-short -count=1`); no `golangci-lint` → `go vet` + `gofmt -l` substitute;
`swag init` broken on unrelated files → swagger equivalents hand-edited
(`docs/swagger.json`, `docs/swagger.yaml`, `docs/docs.go` via
`C:\Python314\python.exe` JSON-safe: 5 new paths + activity/positions
descriptions + 5 stub definitions mirrored from `swagger.json`).

---

## 20. Scale, Perf & Nightly

### 20.1 Sync scale (pool, adaptive pacing, tiers)

Design: SyncAll fans out over W workers (default 4) sharing one venue rate
limiter; pacer starts at the client interval, doubles on 429/rate-limit class
(up to a cap), decays to floor on success streaks; tier by `last_trade_at`
(hot ≤7d → normal cycle, cold → 24h cycle, pending/error-due first);
portfolio fetch follows the wallet's own cycle.

| Case | Function | Input | Expected |
|------|----------|-------|----------|
| SYNC-U-01 | Pool partition | 10 wallets, 4 workers, fake fetch | Each processed exactly once, no dup/skip |
| SYNC-U-02 | Adaptive pacer | 429, 429, then success ×5 | Interval ×2 ×2, then decays to floor; bounded both ends |
| SYNC-U-03 | Tier selection | last_trade now-1d / now-30d / never-synced | hot / cold / pending-first |

| Case | Function | Scenario | Expected |
|------|----------|----------|----------|
| SYNC-I-04 | Pool end-to-end | 20 wallets, pool 4, fake fills | All done, cursors advanced, counts exact |
| SYNC-I-05 | 429 recovery | Upstream 429×2 then OK (real client, fake server) | Attempt gaps grow (backoff), then ready + retry reset |
| SYNC-I-06 | Cold tier skip | Cold wallet, interval not due | Skipped this cycle; processed when due |

### 20.2 Funding collector (store-on-change)

Design: per (venue, instrument) compare incoming rate with last stored row;
store when the rate differs OR the last row is older than the 1h heartbeat
(chart continuity); otherwise skip. Kills ~99% duplicate rows at current
cadence; 90d retention stays as the safety net.

| Case | Function | Input | Expected |
|------|----------|-------|----------|
| FUND-U-01 | shouldStore decision | Same rate + fresh heartbeat / changed rate / stale heartbeat | skip / store / store |

| Case | Function | Scenario | Expected |
|------|----------|----------|----------|
| FUND-I-01 | Collector cycle | Unchanged rates → no rows; changed rate → 1 row; heartbeat due → 1 row | Matches decision table end-to-end |
| FUND-I-02 | Retention endpoint green | Full cleanup after orderbook funcs removed | 200, no error |

### 20.3 Perf suite (tag `perf`)

Deterministic synthetic seeder (fixed seed): N wallets + 30D period rows with
varied metrics. Default N=100k (`PERF_WALLETS` env overrides to 500k/1M).
Run: `go test -tags=perf -run TestPerf_ ./internal/trader/`.

| Case | Function | Scenario | Expected |
|------|----------|----------|----------|
| PERF-01 | Scanner p95 (100k) | 50 mixed queries (AND + sort + cursor walk) | p95 documented (measured 115ms dev, target <500ms); EXPLAIN uses indexes, no seq scan on period metrics |
| PERF-02 | Sync throughput | 1k wallets, fake fetch, pool on | ~5900/min measured, zero failures/duplicates |
| PERF-03 | WS burst 100k events | 10k unique ×10 deliveries | 100k in ~5.5s measured; bounded memory, drops counted, all unique land |

### 20.4 Nightly upstream checks (tag `nightly`, CI cron 2AM)

Read-only live shape checks (no DB writes): fail loudly on upstream drift so
daytime pipelines never silently ingest garbage. CI: `.github/workflows/ci-nightly.yml`
(`schedule: cron '0 2 * * *'`, `go test -tags=nightly ./internal/hyperliquid/`).

| Case | Function | Scenario | Expected |
|------|----------|----------|----------|
| NIGHTLY-01 | Leaderboard shape | Live stats API | `leaderboardRows` present; row keys + day/week/month/allTime windows |
| NIGHTLY-02 | WS frame shape | Dial + subscribe BTC, read 1 frame | `users` has exactly 2 addresses |
| NIGHTLY-03 | Portfolio shape | Live portfolio for a known trader | 8 windows with `accountValueHistory` pairs |
| NIGHTLY-04 | Universe vs cap | Live meta universe count | Non-empty and below subscription cap (early warning) |

---

## Summary

| Module | Unit | Handler | Integration | E2E | Total |
|--------|------|---------|-------------|-----|-------|
| Auth | 51 | 30 | 25 | 13 | **119** |
| Venue | 10 | 8 | 5 | 0 | **23** |
| Instrument | 9 | 8 | 5 | 0 | **22** |
| Market | 16 | 5 | 5 | 0 | **26** |
| OrderBook | 26 | 4 | existing | 0 | **30+** |
| UnifiedState | 24 | 3 | 0 | 0 | **27** |
| Opportunity | 27 | 4 | 0 | 0 | **31** |
| Strategy | 13 | 9 | existing | 0 | **22+** |
| Risk | 30 | 2 | existing | 0 | **32+** |
| Execution | 21 | 4 | existing | 0 | **25+** |
| Reconciliation | 17 | 3 | existing | 0 | **20+** |
| Storage | 17 | 2 | 0 | 0 | **19** |
| FundingArb | 7 | 2 | 0 | 0 | **9** |
| Collector | 4 | 0 | 2 | 0 | **6** |
| Trader Scanner v1.1 | 30 | 13 | 22 | 4 | **69** |
| Wallet Tabs v1 WS1-WS6 (§19.11) | 16 | 12 | 6 | 2 | **36** |
| Sync Scale | 3 | 0 | 3 | 0 | **6** |
| Perf & Nightly (perf/nightly tags) | - | - | - | - | **7** |
| Cross-module | - | - | - | 5 | **5** |
| Security | - | - | - | 6 | **6** |
| **TOTAL** | **~316** | **~95** | **~69** | **~28** | **~516** |
