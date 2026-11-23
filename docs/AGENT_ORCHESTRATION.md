# Agent Orchestration Policy — Risk-based Adaptive Orchestration (RAO) v1

> **Mirrored copy; keep both repos in sync. Version v1.**
> Identical content is maintained in `go_be_arbitrage/docs/AGENT_ORCHESTRATION.md` and `FE_arbitrage/docs/AGENT_ORCHESTRATION.md`.

- **Status:** Approved — v1
- **Date:** 2026-11-23
- **Change control:** Changes require human approval, a version bump, and a mirrored sync across both repos. Workers and reviewers are read-only on this file. The system prompt and global agent configuration are human-only.

## 1. Scope and precedence

This policy defines how agent work is classified, planned, delegated, reviewed, tested, and approved in the `go_be_arbitrage` (BE) and `FE_arbitrage` (FE) repositories.

Precedence, highest first:

1. **Hard rules** — human approval gates, contract gate, no self-approval, commit approval.
2. **This policy** (RAO v1).
3. **Repo `AGENTS.md`**.
4. **Task instructions**.

Lower layers cannot override hard rules. If a lower layer conflicts with a higher layer, the higher layer wins and the conflict must be escalated to a human.

## 2. Principles

- **Classify by description and actual diff.** The tier is based on what the task says AND what the change actually touches. A small-sounding task that touches risky code is not small.
- **Fail safe.** Uncertainty escalates upward. When in doubt, choose the higher tier.
- **Monotonic escalation.** Tier can be raised at any time. No downgrade is allowed after critical detection without recorded human justification.
- **One worker by default.** Use a single worker unless there is a concrete reason to batch.
- **Batch only for real dependencies or file-ownership conflicts.** Do not create empty parallelism.
- **Review by risk.** Review depth follows tier and risk, not habit.
- **Targeted tests first.** Run the smallest relevant checks first and expand only as needed. Reuse results when the code hash is unchanged.
- **Preserve human gates.** Agents never approve their own plans, diffs, or commits.

## 3. Tiers

### L1 — Simple

Localized UI/style/copy change or trivial fix. All of:

- ≤ 2 files changed.
- No risk flags.
- No shared component.

### L2 — Standard

Component, business logic, or multi-file feature work with no risk flags. Typically 3–8 files.

### L3 — Critical

Any of:

- Any risk flag (below).
- More than 8 files changed.
- Cross-repo, contract-affecting work.
- Architectural change.

### Risk flags

| Flag | Areas / signals |
| --- | --- |
| API contract | `contract.md`, `docs/swagger.*`, routes, public JSON |
| DB / migrations | `migrations/**` |
| Auth / authz | `internal/auth/**`, JWT/SIWE, middleware, roles, `src/lib/stores/auth.ts` |
| Money / trading | `internal/execution/**`, `internal/risk/**`, `internal/strategy/**`, `internal/opportunity/**`, funding/PnL/fee math |
| Concurrency | goroutines/channels/mutex/pools, WebSocket server, rate limiter |
| Shared / architecture | `components/shared/**`, `lib/**`, `services/**`, `infrastructure/api-client`, cache, WS layer, cross-feature imports |

Any single flag is sufficient for L3.

### Decision procedure

1. Read the task description and record the claimed tier and rationale.
2. Inspect the planned files, then the actual diff once code exists.
3. Apply the tier definitions and risk flags mechanically.
4. If the diff is broader than described, reclassify before continuing.
5. If uncertain, raise the tier.
6. Record the final tier and evidence in the Task Card. If raised, emit an Escalation Note.
7. After a critical signal is detected, the task stays L3 unless a human records explicit justification for a downgrade.

### Escalation triggers

- Scope drift (new files/areas not in the Task Card).
- Any new or changed API/contract surface.
- Any migration.
- Any auth/authz, money/execution, or concurrency change.
- A newly discovered architectural or cross-repo dependency.

## 4. Workflow per tier

| Tier | Plan | Workers | Reviewer | Tests | Full suite | Docs | Human gates |
| --- | --- | --- | --- | --- | --- | --- | --- |
| L1 | No plan doc | 1 worker | None; supervisor verification checklist | Targeted tests + type-check/lint | Not required | No docs artifact | Commit approval |
| L2 | Mini-plan (scope + acceptance criteria) | 1 worker (2 only if disjoint file ownership) | Mandatory by default | Targeted + relevant regression | At most once at the end, when needed | Only if routes/config/contract change | Commit approval |
| L3 | Detailed plan + human approval | Batches only when justified | Mandatory | Unit + handler + integration + e2e + invariants/race as applicable | As required by risk | `TESTPLAN`/swagger/README as needed | Plan approval + contract approval + commit approval |

Notes:

- L1 reviewer is replaced by the supervisor verification checklist in §6.
- L2 may use two workers only when ownership is disjoint and the split is recorded in the Task Card.
- L3 contract approval happens before any API code is written.
- The commit gate is always human, at every tier.

## 5. Agent responsibilities

**Supervisor**
- Classify the task and record the tier.
- Produce the plan (none for L1, mini for L2, detailed for L3).
- Delegate work and enforce file ownership.
- Verify results and complete the supervisor checklist for L1.
- Enforce gates; never self-approve.

**Worker**
- Implement only the assigned scope.
- Follow existing repo conventions.
- Run the required checks and report exact commands/results.
- Report scope drift, risks, and blockers immediately.
- Never approve its own plan or commit, and never change the tier.

**Code Reviewer (read-only)**
- Review the diff against the Task Card and policy.
- Check invariants, contracts, tests, and risk flags.
- Return a Review Verdict; do not edit code.
- Never approve its own work or change the tier.

**Human**
- Approve L3 plans and any API contract change.
- Approve every commit/push.
- Resolve escalations and record downgrade justifications.
- Own changes to this policy and to system/global configuration.

## 6. Handoff contracts

All handoffs are written artifacts. Empty or missing fields block the handoff.

### Task Card

```
task_id:
title:
repo(s):
tier: L1 | L2 | L3
tier_rationale:            # description + expected diff
scope:                     # files/areas, owned by this worker
acceptance_criteria:
risk_flags:                # none, or list with evidence
dependencies:              # only for batches
worker:
reviewer_required: yes | no (+ reason)
tests_required:            # commands/suites
human_gates:               # plan / contract / commit
plan_link:                 # L2 mini-plan / L3 detailed plan
```

### Worker Report

```
task_id:
tier:
status: complete | blocked | escalated
files_changed:             # path + add/modify/delete
diff_summary:
tests_run:                 # command, result, tree hash, reused?
risk_flags_encountered:
deviations:                # scope drift or none
open_questions:
recommended_next_step:
```

### Review Verdict

```
task_id:
reviewer:
verdict: approve | request_changes | escalate
findings:                  # severity + file:line + rationale
invariants_checked:        # money/execution/auth/concurrency/contract/migration
test_adequacy:
required_fixes:
re_review_required: yes | no
```

### Escalation Note

```
task_id:
from_tier: -> to_tier:
trigger:                   # scope drift / new contract / migration / risky area / architecture
evidence:                  # files, diff, links
impact:
actions_taken:
human_justification:       # required for any downgrade
approvals:
```

### Supervisor Verification checklist (L1)

- [ ] Tier is L1 per policy, and the actual diff matches the description.
- [ ] ≤ 2 files changed; no shared component; no risk flags.
- [ ] Type-check/lint/build pass.
- [ ] Targeted checks pass for all touched logic.
- [ ] No behavior, contract, auth, money, concurrency, or migration change.
- [ ] No required test is missing.
- [ ] Human commit approval is still pending.
- [ ] Tier and evidence are recorded in the Task Card.

## 7. Review policy

A reviewer is **mandatory** for:

- API contracts.
- Migrations.
- Auth/authz.
- Money/trading execution.
- Concurrency/races.
- Critical business logic.

**L2:** reviewer is mandatory by default.

**Narrow L2 exemption.** A reviewer may be skipped ONLY if ALL of the following are true:

- ≤ 2 files changed.
- All files are new (no edits to existing files).
- Leaf UI only.
- No imports from `components/shared`, `lib`, `services`, `infrastructure`, or shared hooks.
- No new or changed exported symbol.
- No test edits.
- No behavior change.
- The supervisor records the justification in the Task Card.

**L1:** the supervisor verification checklist in §6 replaces the reviewer.

## 8. Testing policy

Sequence:

1. Targeted tests for the touched code.
2. Relevant regression tests.
3. Full suite only when required by tier or risk.

Reuse results only when the tree hash (code state + command) is unchanged. Never skip critical invariant tests for money, execution, auth, concurrency, contracts, or migrations — even if a cache entry exists.

### Frontend commands

```bash
pnpm vitest run <path>          # targeted unit
pnpm type-check
pnpm lint
npx playwright test <spec>      # targeted e2e
pnpm test                       # full unit suite
pnpm test:e2e                   # full e2e suite
```

Visual snapshots only with explicit approval.

### Backend commands

```bash
go test -short ./internal/<pkg>/...   # targeted
go vet ./...
golangci-lint run ./...
go test -short -count=1 ./...         # full
go test -tags=integration -count=1 ./...   # L3 integration
go test -tags=e2e -count=1 ./...           # L3 e2e
```

## 9. Human approval gates

- L3 plan.
- Any API contract change.
- Commit/push.

No worker or supervisor may self-approve any gate. Approval must be explicit and recorded.

## 10. Efficiency rules

- No unnecessary batches.
- No sequential workers when one suffices.
- Parallelize only disjoint file ownership.
- No reviewer for trivial L1 work.
- No full suite after unrelated fixes.
- No docs artifacts for trivial tasks.

## 11. Metrics and baseline

Track per task:

- Wall-clock time.
- Agent calls.
- Tool calls.
- Full-suite runs.
- Escaped defects.

Baseline from the latest task (scanner redesign):

- 6 workers.
- 4 reviewer calls + 2 retries.
- 4 fix rounds.
- ~10 targeted runs.
- FE unit: 660 tests / 73 files.
- FE e2e: 138 tests / ~57s.
- 9 visual snapshots.
- BE unit: all packages.
- 1 docs artifact.
- Wall-clock: not captured.

## 12. Policy change control

- Human approval is required.
- Bump the version.
- Mirror the change to both repos in the same change set.
- Add a changelog entry.
- Workers and reviewers are read-only on this file.

## 13. Acceptance criteria and scenario suite

### Acceptance criteria

- **AC1 — Mirrored policy exists.** `docs/AGENT_ORCHESTRATION.md` exists in both repos with identical bodies, a mirrored-copy header, and version v1.
- **AC2 — Precedence enforced.** Hard rules > policy > `AGENTS.md` > task instructions; lower layers cannot override hard rules.
- **AC3 — Tiering is deterministic.** L1/L2/L3 definitions and risk flags are explicit; classification uses description AND actual diff.
- **AC4 — Escalation is safe.** Uncertainty escalates; critical detection is monotonic; no downgrade without recorded human justification.
- **AC5 — Workflow matches tier.** Plan, workers, reviewer, tests, full-suite use, docs, and gates follow the per-tier table.
- **AC6 — Handoffs are complete.** Task Card, Worker Report, Review Verdict, Escalation Note, and supervisor checklist are used and complete.
- **AC7 — Review and testing are risk-based.** Mandatory review cases, the narrow L2 exemption, targeted-first testing, hash-based reuse, and invariant coverage are enforced.
- **AC8 — Gates and change control hold.** L3 plan, contract, and commit/push approvals are human-only; both `AGENTS.md` files reflect the policy.

### Scenario suite

| ID | Scenario | Expected outcome |
| --- | --- | --- |
| T1 | Copy/style fix in one leaf file, no risk flags | L1: 1 worker, supervisor checklist, targeted checks, no full suite, human commit gate. |
| T2 | "Small" change that touches `components/shared/**` | Risk flag raised; reclassify to L3 before implementation. |
| T3 | New standard component or multi-file feature in 3–8 files, no flags | L2: mini-plan, 1 worker, mandatory reviewer, targeted + regression tests, hash-based reuse. |
| T4 | Two L2 tasks with disjoint file ownership | Batch of 2 workers allowed, ownership recorded; reviewer still mandatory. |
| T5 | Scope drift during L1 (new API call appears in diff) | Halt, escalate to L3, contract gate applies; no silent continuation. |
| T6 | Route/swagger/public JSON change | L3; human contract approval before API code. |
| T7 | New migration | L3; mandatory reviewer; integration tests and invariant coverage. |
| T8 | Auth/JWT/SIWE/middleware/role change | L3; mandatory reviewer; auth invariants. |
| T9 | Funding/PnL/fee or execution/risk/strategy change | L3; mandatory reviewer; money invariants never skipped. |
| T10 | Goroutine/channel/mutex/WS server/rate limiter change | L3; mandatory reviewer; race checks. |
| T11 | Architectural or cross-repo dependency discovered mid-task | Escalate to L3; update plan and gates. |
| T12 | Downgrade attempted after critical detection | Blocked; requires recorded human justification. |
| T13 | L2 narrow exemption candidate (≤ 2 new leaf UI files, no shared imports, no exports/tests/behavior change) | Reviewer may be skipped only with all conditions met and justification recorded. |
| T14 | Worker attempts commit/push or L3 plan/contract without approval | Blocked by hard gate; human approval required. |

## Changelog

- **v1 — 2026-11-23:** Initial RAO policy; mirrored in BE and FE repositories.
