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
| Collector | 3 | 0 | 0 | 0 | **3** |
| Cross-module | - | - | - | 5 | **5** |
| Security | - | - | - | 6 | **6** |
| **TOTAL** | **~271** | **~84** | **~40** | **~24** | **~419** |
