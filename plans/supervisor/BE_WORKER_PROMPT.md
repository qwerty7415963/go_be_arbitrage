# BE Worker Prompt (OpenCode)

## Goal
Implement the backend for `<feature>` strictly against `plans/<feature>/contract.md` (frozen vX).

## Allowlist
- `go_be_arbitrage/**` only.
- Do NOT edit FE repo, contract, or `plans/supervisor/**`.
- Do NOT run `git commit` or `git push`.

## Inputs
- Contract: `plans/<feature>/contract.md` (version vX).
- Read `AGENTS.md` and `docs/TESTPLAN.md`.
- Branch: `feat/<feature>-be`.

## Acceptance checklist
- [ ] All contract endpoints implemented with request/response shapes exactly as specified.
- [ ] Validation, authz, pagination, error codes per contract.
- [ ] Migrations (if any) have up+down and are applied/rolled back successfully.
- [ ] No FE assumptions; no invented fields.
- [ ] Tests added: unit + handler + integration for touched code.
- [ ] `gofmt`, `go vet ./...`, `golangci-lint run ./...` clean.
- [ ] `go test -short -race -count=1 ./internal/<pkg>/...` passes.
- [ ] `go test -tags=integration -race -count=1 ./internal/<pkg>/...` passes (if DB).
- [ ] `docs/TESTPLAN.md` updated (L2/L3 when behavior/contract changes).
- [ ] `swag init` if endpoints changed.
- [ ] Files exist and `git diff --stat` reviewed.

## Report (impl checkpoint)
Use `plans/supervisor/CHECKPOINT_SCHEMA.md` (Impl checkpoint). Include:
Feature | Contract version | Branch | Commit SHA (human later) | Files changed | Endpoints implemented | Tests run | Evidence | Known risks.

## Stop conditions
- Contract ambiguity → stop, report to orchestrator; do not guess.
- Need to change contract → stop (orchestrator + human freeze vX+1).
- Need to edit outside allowlist → stop, report.
- Test flaky → retry max 2, then report.

## Session policy (v2.1)
- This role uses ONE long-lived session per feature. Orchestrator stores the sessionID in `plans/<feature>/sessions.md`.
- Read the current batch from `plans/<feature>/queue.md`; do not wait for a new session.
- Read the contract from `plans/<feature>/contract.md`; do not rely on pasted contract text.
- Run targeted tests inside this batch; report the impl/QA checkpoint.
- Do not spawn subagents yourself. Do not commit or push.
- If the provider returns 429/502: stop, report `BLOCKED: rate-limit`; do not retry more than 2 times.
