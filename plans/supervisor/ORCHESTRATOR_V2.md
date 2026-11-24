# Orchestrator v2 — Planning + Contract + Final Review only

> Orchestrator không đứng giữa FE và BE. Nó chỉ làm 3 việc:
> 1. Planning
> 2. Contract
> 3. Final review
>
> FE và BE chạy song song theo cùng một contract đã freeze.

## 1. Task types

Mỗi feature lớn chia thành DAG 3 loại task:

- **A. Parallel**: FE, BE, mock/fixtures chạy cùng lúc, build theo **một contract đã freeze**.
- **B. Dependency**: chỉ chạy khi dependency xanh:
  - Integration ← (BE done + FE done)
  - QA ← Integration green
  - DONE ← QA pass
- **C. Integration**: conformance test. Cùng một bộ assert chạy 2 lần:
  1. trên mock
  2. trên real BE

```text
REQUIREMENTS
  → CONTRACT (frozen vX)
  → MOCK (generated from contract)
        │     │
        ↓     ↓ (A. Parallel)
     BE worker → impl checkpoint
     FE worker → impl checkpoint
        │     │
        └─────┬─────┘
              ↓ (C. Integration)
        conformance: mock ≡ real BE?
              ↓ (B. Dependency)
        QA agent: gates + e2e + QA checkpoint
              ↓
        Orchestrator final review
              ↓
        Human commits + pushes
```

## 2. Agents và ranh giới cứng (OpenCode)

| Agent | Công cụ | Input | Output | Forbidden |
|---|---|---|---|---|
| Orchestrator | session chính (read/grep/glob + subagent + question) | Requirements | Planning + contract vX + final review | verify line-by-line; sửa code; commit/push |
| BE worker | `subagent(agent: general)` + `BE_WORKER_PROMPT.md` | contract vX | code + impl checkpoint (BE repo) | đoán FE; đổi contract; commit/push |
| FE worker | `subagent(agent: general)` + `FE_WORKER_PROMPT.md` | contract vX + mock | code + impl checkpoint (FE repo) | đoán BE; invent field; đổi contract; commit/push |
| QA agent | `subagent(agent: general)` + `QA_AGENT_PROMPT.md` | contract vX + both repos | conformance + gates + QA checkpoint | đọc business logic; sửa code |
| Risk reviewer | `subagent(agent: code-reviewer)` | flagged diff | review read-only | sửa code |
| Human | — | acceptance report | approve + commit/push | — |

Repo paths (dự án này):
- BE: `go_be_arbitrage/`
- FE: `FE_arbitrage/`

Dispatch rule (OpenCode):
- Dispatch mới: **không truyền `sessionID`**. Chỉ truyền `ses_...` thật để follow-up worker đã có.
- Nếu tool trả "Subagent session not found" → coi như chưa chạy, dispatch lại ngay.
- Sau mỗi dispatch: verify file tồn tại + `git diff --stat` + test targeted. Worker báo done mà file không có → fail, re-dispatch nhỏ hơn.
- Mirror tài liệu `plans/supervisor/` ở cả 2 repo, byte-identical (verify SHA-256).

## 3. Contract change rule

Không được silently patch contract giữa chừng.

Nếu cần đổi contract:
1. Freeze contract **vX+1**.
2. Orchestrator viết contract mới; human approve.
3. Rebuild mock từ vX+1.
4. Rerun cả BE và FE từ impl checkpoint.
5. Checkpoint phải mang `contract version` để trace skew.
6. Branch báo cáo theo version cũ → **BLOCKED**, không phải failed.

## 4. Risk-based review

QA không đọc code mặc định. Review hẹp auto-trigger khi diff chạm:

- **concurrency**: goroutines, locks, channels, WS pumps, reconnect
- **money**: amounts, PnL, fees, balances, order sizing
- **migrations**: bất kỳ file `migrations/**`
- **auth**: tokens, secrets, signatures, password flows

QA chỉ hand flagged files cho `code-reviewer`; không tự review business logic.

## 5. M-rules (standing)

- **M1 — no commits.** Worker và QA không chạy `git commit` / `git push`. Chỉ human commit/push sau khi approve final report.
- **opencode.json guard.** Luôn untracked; không bao giờ stage. Thêm vào `.gitignore` nếu tạo.
- **M-commit-gate.** `git status` là một QA gate. Commit lén, plan copy còn sót, hoặc `opencode.json` staged → fail ngay.

## 6. Per-feature loop

1. Orchestrator viết `plans/<feature>/contract.md` (version v1).
2. Human approve contract.
3. Orchestrator generate/update mock từ contract (FE fixtures + conformance harness).
4. Dispatch BE + FE workers song song.
5. Workers report impl checkpoints.
6. Dispatch QA agent: conformance + full gates.
7. QA report QA checkpoint.
8. Product FAIL → trả về worker đúng repo kèm raw logs.
9. Env BLOCKED → sửa môi trường rồi rerun QA.
10. Orchestrator final review (checkpoints + conformance only).
11. Orchestrator acceptance report.
12. Human commits + pushes từng repo riêng.

## 7. Directory layout

```
plans/
  supervisor/
    ORCHESTRATOR_V2.md
    BE_WORKER_PROMPT.md
    FE_WORKER_PROMPT.md
    QA_AGENT_PROMPT.md
    CHECKPOINT_SCHEMA.md
    contract-template.md
  <feature>/
    contract.md
    checkpoints/
      BE.md
      FE.md
      QA.md
```

Canonical ở `go_be_arbitrage/plans/`; mirror byte-identical ở `FE_arbitrage/plans/` (verify SHA-256).

## 8. Contract template

Dùng `plans/supervisor/contract-template.md` (9 mục: Scope, Endpoints, DTOs, Validation, Pagination, Caching, Permissions, Test expectations, Version).

Contract không có API → ghi rõ "No API changes" và bỏ qua mock/conformance.

## 9. Impl checkpoint

Format: `plans/supervisor/CHECKPOINT_SCHEMA.md` (mục Impl checkpoint). Lưu tại `plans/<feature>/checkpoints/BE.md` và `FE.md`.

Bắt buộc có:
- Feature, Contract version, Branch, Commit SHA (human commit sau), Files changed, Endpoints implemented, Tests run, Evidence (`git diff --stat`, file paths), Known risks.

## 10. QA checkpoint

Format: `plans/supervisor/CHECKPOINT_SCHEMA.md` (mục QA checkpoint). Lưu tại `plans/<feature>/checkpoints/QA.md`.

Bắt buộc có:
- Contract version
- Conformance mock vs real BE: PASS/FAIL
- Gates: type-check, lint, format, unit, build, e2e
- Product: PASS/FAIL
- Env: OK/BLOCKED
- Failed tests + raw logs

## 11. Acceptance report

Format: `plans/supervisor/CHECKPOINT_SCHEMA.md` (mục Acceptance report). Orchestrator tạo sau QA pass.

## 12. Cách dùng cho dự án khác

1. Copy thư mục `plans/supervisor/`.
2. Tạo `plans/<feature>/contract.md`.
3. Human approve contract.
4. Generate mock từ contract.
5. Dispatch BE worker + FE worker song song.
6. Dispatch QA agent.
7. Human review acceptance report.
8. Human commit/push từng repo.

Nguyên tắc cốt lõi: **contract freeze trước khi code, worker không đoán bên kia, QA không sửa code, human mới commit.**

## 13. OpenCode notes

- `AGENTS.md` trỏ tới file này; `plans/supervisor/*` không auto-load.
- FE/BE worker chạy `subagent(general)`; QA chạy `subagent(general)` roadmap riêng; risk review dùng `subagent(code-reviewer)`.
- Test thật:
  - BE: `go vet ./...`, `golangci-lint run ./...`, `go test -short -race -count=1 ./internal/<pkg>/...`, `go test -tags=integration -race -count=1 ./internal/<pkg>/...`, `go test -tags=e2e -race -count=1 ./...`.
  - FE: `pnpm type-check`, `pnpm lint`, `pnpm vitest run <path>`, `pnpm test`, `npx playwright test <spec>`, `pnpm test:e2e` (cần BE `:8080` + FE `:3000`).
- QA gate bổ sung: mirror hash, `git status` (M-commit-gate), contract version trong checkpoint.
