# AGENTS.md — Development Workflow

## Orchestration Policy (RAO v1)

See `docs/AGENT_ORCHESTRATION.md` (v1). Tiers: L1 Simple / L2 Standard / L3 Critical. Hard gates: L3 plan approval, API contract approval, commit/push approval. Workers/reviewers are read-only on policy and never self-approve. Classification is by task description AND actual diff; uncertainty escalates upward; no downgrade after critical detection.

## Mandatory Rule: Tests by Tier

**Classify every task (L1/L2/L3) using `docs/AGENT_ORCHESTRATION.md` before starting.**

- **L1:** Targeted unit/check for touched logic + `go vet`; no TESTPLAN artifact required; full suite not required.
- **L2:** Unit + handler tests for touched code, relevant regression, TESTPLAN updated when behavior/contract changes.
- **L3:** Full matrix (unit/handler/integration/e2e) + TESTPLAN + swagger as applicable.

Never skip invariant tests for money/execution/auth/concurrency/migrations.

For L2/L3, before ANY code is merged, the following MUST be complete:

### 1. Test Plan (write FIRST)
- File: `docs/TESTPLAN.md`
- For each new feature, add test cases before writing code
- Format: case ID, description, input, expected output
- Organize by level: Unit → Handler → Integration → E2E

### 2. Unit Tests
- File: `internal/<module>/*_test.go`
- Every exported function must have at least 1 test
- Cover: happy path, error cases, edge cases, boundary values
- Use mock for external dependencies (DB, exchange APIs)
- Run: `go test -short -race -count=1 ./internal/<module>/...`

### 3. Handler Tests (HTTP layer)
- File: `internal/<module>/handler_test.go`
- Test every HTTP endpoint
- Cover: valid input, invalid input, missing auth, wrong role, edge cases
- Use httptest + gin.TestMode
- Run: `go test -short -race -count=1 ./internal/<module>/...`

### 4. Integration Tests (database required)
- File: `internal/<module>/*_integration_test.go`
- Build tag: `//go:build integration`
- Test repository layer against real PostgreSQL
- Cover: CRUD, constraints, joins, transactions
- Run: `go test -tags=integration -race -count=1 ./internal/<module>/...`

### 5. E2E Tests (full flow)
- File: `e2e_test.go` (root level)
- Build tag: `//go:build e2e`
- Test complete user flows across modules
- Cover: happy path end-to-end, cross-module interactions
- Run: `go test -tags=e2e -race -count=1 ./...`

### 6. Documentation Update
- Update `docs/TESTPLAN.md` with new test cases
- Update `docs/swagger.json` if new endpoints added: `swag init`
- Update `README.md` if new features/endpoints

## Test Naming Convention

- Unit: `TestFunctionName_Scenario` (e.g., `TestRegister_DuplicateEmail`)
- Handler: `TestHandler_Endpoint_Scenario` (e.g., `TestHandler_Login_WrongPassword`)
- Integration: `TestRepo_Method_Scenario` (e.g., `TestRepo_CreateUser_Success`)
- E2E: `TestE2E_Flow_Description` (e.g., `TestE2E_Auth_FullFlow`)

## Before Committing — Checklist

- [ ] Tier recorded in Task Card/PR; checks match tier (see `docs/AGENT_ORCHESTRATION.md`).
- [ ] `go vet ./...` passes (all tiers)
- [ ] Targeted `go test -short` for touched packages passes (all tiers); full `go test -short -race -count=1 ./...` for L2/L3 as applicable
- [ ] `go test -tags=integration -race -count=1 ./...` passes (L3 unless risk requires; if DB available)
- [ ] `go test -tags=e2e -race -count=1 ./...` passes (L3 unless risk requires; if DB available)
- [ ] `swag init` if endpoints changed (L2 when behavior/contract changes; L3 as required)
- [ ] `docs/TESTPLAN.md` updated with new test cases (L2 when behavior/contract changes; L3 as required)
- [ ] No secrets or keys in code

## Run All Checks

```
make check           # fmt + vet + lint
make test-unit       # unit tests
make test-integration  # integration tests (needs DB)
make test-e2e        # e2e tests (needs DB)
make ci              # full CI: fmt + vet + lint + unit + build
```
