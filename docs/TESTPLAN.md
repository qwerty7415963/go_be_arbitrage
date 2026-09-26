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

## 17. Wallet Dashboard Feature (`internal/walletgroup/`, `internal/wallet/`)

> Plan reference: `arbitrage-platform-docs/WALLET_DASHBOARD_PLAN.md`
> Source doc: `arbitrage-platform-docs/Perp_Wallet_Dashboard_Backend_Tasks_v1.0.docx`
> Cases written BEFORE implementation (AGENTS.md workflow). BR-xx = business rules, BE-xx = docx tasks.

### 17.0 Phase 0 — Migration Runner (golang-migrate)

| Case | Function | Scenario | Expected |
|------|----------|----------|----------|
| MIG-01 | `cmd/server migrate up` | Empty DB | All 000001..0000NN apply, version = latest |
| MIG-02 | `cmd/server migrate down` | After up | Rollback last migration, no error |
| MIG-03 | `cmd/server migrate version` | Applied DB | Prints current version |
| MIG-04 | `migrate up` | Already up-to-date | No-op, exit 0 |
| MIG-05 | Integration | Test DB (`:5433`) applies all migrations cleanly | Tables exist, down works |

### 17.1 Phase 1 — WalletGroup Unit Tests

| Case | Function | Scenario | Expected |
|------|----------|----------|----------|
| GRP-U-01 | Address normalization | `" 0xABC..def "` | Trimmed, EVM checksum/canonical form (BE-01) |
| GRP-U-02 | Address normalization | Non-hex / short string | `WALLET-002` validation error |
| GRP-U-03 | Chain normalization | `"1"`, `"1"` (same chain, different input) | Same canonical chain id → same identity (BR-01) |
| GRP-U-04 | Group name validation | `""` / `"   "` | `COMMON-902` field error on `name` (BE-07) |
| GRP-U-05 | CreateGroup service | Valid name, unique | Group created, returns id + metadata |
| GRP-U-06 | CreateGroup service | Duplicate name, same user | `GROUP-002` conflict (BE-07) |
| GRP-U-07 | CreateGroup service | Duplicate name, different user | Allowed (unique per user only) |
| GRP-U-08 | IsGroupOwnedBy | Owner vs non-owner | true / false (BE-10, service layer) |
| GRP-U-09 | DeleteGroup service | Existing group | Group + membership rows deleted; wallet rows untouched (BR-10) |
| GRP-U-10 | AddWallet bulk | Valid wallet ids | All memberships created |
| GRP-U-11 | AddWallet duplicate | Same wallet added twice | Idempotent success, exactly 1 row (BR-09, BR-03) |
| GRP-U-12 | AddWallet unknown wallet | Random uuid | `WALLET-001` (BE-08) |
| GRP-U-13 | RemoveWallet absent | Wallet not in group | Idempotent success (no-op), 204 |
| GRP-U-14 | Wallet multi-group | Add same wallet to 2 groups | Both memberships exist (BR-02) |
| GRP-U-15 | Membership count | Group with N wallets | `wallet_count = N`, no N+1 (BE-12) |

### 17.2 Phase 1 — WalletGroup Handler Tests

| Case | Endpoint | Scenario | Expected |
|------|----------|----------|----------|
| GRP-H-01 | POST /groups | Valid body | 201 + group id |
| GRP-H-02 | POST /groups | Blank name | 400 `COMMON-902` |
| GRP-H-03 | POST /groups | Duplicate name | 409 `GROUP-002` |
| GRP-H-04 | POST /groups | No JWT | 401/403 |
| GRP-H-05 | GET /groups | User A has 2 groups | Only A's groups in list |
| GRP-H-06 | GET /groups/:id | Own group | 200 + wallet_count |
| GRP-H-07 | GET /groups/:id | User B's group | 403 `GROUP-003` or 404 (no existence leak, BE-10) |
| GRP-H-08 | GET /groups/:id | Unknown id | 404 `GROUP-001` |
| GRP-H-09 | PATCH /groups/:id | Own group, new name | 200 |
| GRP-H-10 | PATCH /groups/:id | Cross-user | 403/404 |
| GRP-H-11 | DELETE /groups/:id | Own group | 204; membership gone, wallet rows remain |
| GRP-H-12 | DELETE /groups/:id | Delete twice | 404 on second (or idempotent 204 — consistent per convention) |
| GRP-H-13 | POST /groups/:id/wallets | Single valid wallet | 200 + membership |
| GRP-H-14 | POST /groups/:id/wallets | Bulk 10 wallets | 200, 10 memberships |
| GRP-H-15 | POST /groups/:id/wallets | Duplicate wallet in body + existing | Idempotent 200, no dup rows |
| GRP-H-16 | POST /groups/:id/wallets | Unknown wallet id | 404 `WALLET-001` |
| GRP-H-17 | POST /groups/:id/wallets | Cross-user group | 403/404 |
| GRP-H-18 | DELETE /groups/:id/wallets | Remove 1 of 3 | 204, 2 remain |
| GRP-H-19 | GET /groups/:id/wallets | `search=0xabc` partial | Matched wallets only |
| GRP-H-20 | GET /groups/:id/wallets | `page=2&limit=5` | Stable offset pagination |

### 17.3 Phase 1 — WalletGroup Integration Tests (`//go:build integration`)

| Case | Function | Scenario | Expected |
|------|----------|----------|----------|
| GRP-I-01 | CreateGroup | Success | Row in DB, unique constraint active |
| GRP-I-02 | CreateGroup | Duplicate `(user_id, name)` | UNIQUE violation → `GROUP-002` |
| GRP-I-03 | ListGroups | Multiple users | Only caller's groups returned |
| GRP-I-04 | DeleteGroup | Group + members exist | Both tables cleaned; tracked_wallets intact |
| GRP-I-05 | AddMember | Idempotent insert | ON CONFLICT DO NOTHING, 1 row |
| GRP-I-06 | AddMember | PK `(group_id, wallet_id)` | Second insert rejected by constraint |
| GRP-I-07 | Wallet in 2 groups | Cross-join check | Both rows exist, remove from one keeps other (BR-02) |
| GRP-I-08 | RemoveMembers | Bulk remove | Correct rows removed, others untouched |
| GRP-I-09 | GetGroup | Foreign group id | Not-found/forbidden per ownership query |
| GRP-I-10 | Bulk add tx | Partial failure | Transaction rollback, no partial membership (TEST-05) |

### 17.4 Phase 1 — E2E (docx §6 scenarios)

| Case | Flow | Steps | Expected |
|------|------|-------|----------|
| E2E-06 | List isolation | A lists groups | Only A's groups |
| E2E-07 | Create + duplicate | A creates "Smart Money" ×2 | First 201, second 409 `GROUP-002` |
| E2E-08 | Idempotent add | A adds Wallet X twice | Second success, no duplicate row |
| E2E-09 | Multi-group | X in "Smart Money" + "Whale" | Both memberships exist |
| E2E-10 | Isolated remove | Remove X from "Smart Money" | X still in "Whale" |
| E2E-11 | Delete cleanup | Delete "Smart Money" | Memberships gone; X + "Whale" intact (BR-10) |
| E2E-12 | Cross-user attack | B patches/deletes A's group | Blocked 403/404 (TEST-06) |
| E2E-13 | Wallet identity | Same address+chain added twice (normalize) | Same `tracked_wallets` row reused (BR-01) |

### 17.5 Phase 2 — Scanner Unit Tests (filter parser & validation — TEST-01)

| Case | Function | Scenario | Expected |
|------|----------|----------|----------|
| SCAN-U-01 | ParseFilters | Valid enum: dex, chain, timeframe | Parsed |
| SCAN-U-02 | ParseFilters | Unknown DEX / chain / timeframe | `COMMON-902` error |
| SCAN-U-03 | ParseFilters | Numeric ops: gt/gte/lt/lte | Parsed, correct operator |
| SCAN-U-04 | ParseFilters | `between=10,5` reversed | Reject (BE-03) |
| SCAN-U-05 | ParseFilters | `between=abc,5` / negative where disallowed | Reject, no silent coerce |
| SCAN-U-06 | ParseFilters | Decimal + boundary values | Parsed exactly |
| SCAN-U-07 | ParseFilters | Empty / whitespace search | Treated as no search (no error, no full-table term) |
| SCAN-U-08 | ParseFilters | Custom range `start >= end` | Reject (BE-04) |
| SCAN-U-09 | ParseSort | Valid field + order asc/desc | Parsed |
| SCAN-U-10 | ParseSort | Invalid field | `COMMON-902` (BE-05) |
| SCAN-U-11 | Multi-select | `dex=a&dex=b` + comma form | OR semantics (BR-11) |
| SCAN-U-12 | AND combination | 2+ metric filters | AND semantics (BR-11) |

### 17.6 Phase 2 — Metric Semantics Unit Tests (TEST-02)

| Case | Function | Scenario | Expected |
|------|----------|----------|----------|
| SCAN-U-13 | ComputeMetrics | Normal win/loss fixture set | Expected PnL/ROI/win_rate deterministic |
| SCAN-U-14 | ComputeMetrics | Breakeven trade (PnL=0) | Not counted as win |
| SCAN-U-15 | ComputeMetrics | Partial closes, one logical position | Counted as 1 trade |
| SCAN-U-16 | ComputeMetrics | No data for timeframe | All metrics `null`, **never 0** (BR-07) |
| SCAN-U-17 | ComputeMetrics | Long vs short fills | long/short ratio correct |
| SCAN-U-18 | ComputeMetrics | Last Active | max(fill timestamp) UTC (BR-08) |
| SCAN-U-19 | Timeframe isolation | Request 24H vs 7D | Metrics computed only in requested window (BE-04) |

### 17.7 Phase 2 — Scanner Handler Tests

| Case | Endpoint | Scenario | Expected |
|------|----------|----------|----------|
| SCAN-H-01 | GET /wallets | No filters | 200, default sort `pnl desc` (BR-12) |
| SCAN-H-02 | GET /wallets | `search` exact + partial address | Matched only |
| SCAN-H-03 | GET /wallets | Single metric filter `pnl_gt=100000` | Only matching wallets |
| SCAN-H-04 | GET /wallets | AND: `pnl_gt` + `win_rate_gt` | Only wallets satisfying both |
| SCAN-H-05 | GET /wallets | OR: `dex=hyperliquid,gmx` | Either DEX (BR-11) |
| SCAN-H-06 | GET /wallets | NULL-metric wallet + numeric filter | Excluded (BR-07) |
| SCAN-H-07 | GET /wallets | `sort=volume&order=asc` | Correct order |
| SCAN-H-08 | GET /wallets | Ties on sort metric | Deterministic `(chain, address)` tiebreak (BE-05) |
| SCAN-H-09 | GET /wallets | `page=2&limit=10` | No dup/skip across pages |
| SCAN-H-10 | GET /wallets | `timeframe=24H` vs `ALL` | Metrics consistent with timeframe |
| SCAN-H-11 | GET /wallets | Invalid sort/operator/timeframe | 400 `COMMON-902` |
| SCAN-H-12 | GET /wallets/:id | Existing wallet | 200 + identity + metrics |
| SCAN-H-13 | GET /wallets/:id | Unknown | 404 `WALLET-001` (BE-06) |
| SCAN-H-14 | GET /wallets/:id | Wallet in user B's group | No group membership of B leaked (BE-06) |
| SCAN-H-15 | GET /groups/:id/wallets | Filtered query on group | Only current group's wallets filtered (BE-09) |
| SCAN-H-16 | GET /groups/:id/wallets | Filter matches nothing | Empty list, no membership change (BE-09) |

### 17.8 Phase 2 — Scanner Integration Tests (TEST-03, `//go:build integration`)

| Case | Function | Scenario | Expected |
|------|----------|----------|----------|
| SCAN-I-01 | ScanWallets | Fixture: 50 wallets, filters | Only match rows |
| SCAN-I-02 | ScanWallets | AND combination SQL | Correct intersection |
| SCAN-I-03 | ScanWallets | Multi-select OR SQL | Correct union |
| SCAN-I-04 | ScanWallets | Search normalize (casing/whitespace) | Canonical match |
| SCAN-I-05 | ScanWallets | Sort ASC/DESC + page walk | Deterministic, no dup/skip (BE-05) |
| SCAN-I-06 | ScanWallets | Timeframe filter | Rows scoped to timeframe |
| SCAN-I-07 | GetWalletDetail | Matches scanner data same timeframe | Consistent metrics (BE-06) |
| SCAN-I-08 | GroupWallets filter | Group of 10, filter 3 match | 3 rows, others untouched (BE-09) |
| SCAN-I-09 | NULL metric | Snapshot row all-null | Excluded by any numeric filter (BR-07) |

### 17.9 Phase 3 — Ingestion & Metrics Engine

| Case | Function | Scenario | Expected |
|------|----------|----------|----------|
| ING-U-01 | Extended adapter fixture | Valid trader-history payload | Normalized fills persisted |
| ING-U-02 | Extended adapter fixture | Unknown/missing fields | Graceful skip + log, no crash |
| ING-U-03 | Variational adapter fixture | Valid payload | Normalized fills persisted |
| ING-U-04 | Ingestion idempotency | Same fill delivered twice | 1 row (unique venue fill id) |
| ING-I-01 | Backfill worker | Wallet with history | Snapshots computed for all timeframes |
| ING-I-02 | Engine vs fixture | Known fill sequence | PnL/ROI/win-rate match expected fixture values |
| ING-I-03 | Spike gate | No usable venue feed | STOP: documented, no guessed data (not a test — process gate) |

### 17.10 Phase 4 — Hardening (TEST-07/08/09/10)

| Case | Type | Scenario | Expected |
|------|------|----------|----------|
| HARD-01 | Race (`-race`) | 2+ concurrent POST same wallet → same group | No duplicate membership (TEST-07) |
| HARD-02 | Race | Concurrent add + remove | No inconsistent state |
| HARD-03 | Race | Concurrent PATCH/DELETE same group | One wins, one clean conflict |
| HARD-04 | Perf | Scanner common filters @ large dataset | No full scan (EXPLAIN), no N+1 (TEST-08) |
| HARD-05 | Perf | Group detail with many wallets | wallet_count via single aggregate query |
| HARD-06 | Regression | Full existing suite + migrations | All pass (TEST-09) |
| HARD-07 | Contract | Swagger/README examples vs actual response | Schema match (TEST-10) |
| HARD-08 | Observability | Group mutations | Structured log with actor + request id; no secrets (BE-13) |
| HARD-09 | Security | SQL injection via `search`/filters | Parameterized only, no effect |
| HARD-10 | Security | XSS in group name/color | Stored/rendered safely |

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
| Wallet Dashboard (planned) | 34 | 36 | 19 | 8+ | **97** |
| Cross-module | - | - | - | 5 | **5** |
| Security | - | - | - | 6 | **6** |
| **TOTAL** | **~305** | **~120** | **~59** | **~32** | **~516** |
