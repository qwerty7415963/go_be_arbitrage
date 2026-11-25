# FE Worker Prompt (OpenCode)

## Goal
Implement the frontend for `<feature>` strictly against `plans/<feature>/contract.md` (frozen vX) and the generated mock/fixtures.

## Allowlist
- `FE_arbitrage/**` only.
- Do NOT edit BE repo, contract, or `plans/supervisor/**`.
- Do NOT run `git commit` or `git push`.

## Inputs
- Contract: `plans/<feature>/contract.md` (version vX).
- Mock/fixtures: `tests/fixtures/<feature>/`.
- Read `AGENTS.md` and `docs/test-cases.md`.
- Branch: `feat/<feature>-fe`.

## Acceptance checklist
- [ ] All screens/components per contract; no invented fields or endpoints.
- [ ] Loading, empty, error, partial, stale states rendered.
- [ ] i18n keys added to both `src/messages/en.json` and `vi.json`.
- [ ] No hardcoded user-facing text.
- [ ] New interactive UI component → e2e test added (unit cannot catch rendering).
- [ ] `pnpm type-check` passes.
- [ ] `pnpm lint` passes.
- [ ] `pnpm vitest run <targeted paths>` passes.
- [ ] Targeted e2e `npx playwright test <spec>` passes (needs BE `:8080` + FE `:3000`).
- [ ] `docs/test-cases.md` updated (L2/L3 when tests/behavior change).
- [ ] Files exist and `git diff --stat` reviewed.

## Report (impl checkpoint)
Use `plans/supervisor/CHECKPOINT_SCHEMA.md` (Impl checkpoint). Include:
Feature | Contract version | Branch | Commit SHA (human later) | Files changed | Screens/endpoints wired | Tests run | Evidence | Known risks.

## Stop conditions
- Contract ambiguity or missing field → stop, report; do not invent.
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
