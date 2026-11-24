# WORKFLOW — OpenCode Parallel FE/BE Agents

> Bổ sung cho `docs/AGENT_ORCHESTRATION.md` (RAO v1).
> Precedence: hard rules > RAO v1 > contract > file này > AGENTS.md > task.
> Contract: `go_be_arbitrage/plans/wallet-groups/contract-v1.0.md`
> Mirror: `FE_arbitrage/docs/contracts/wallet-groups-v1.0.md` (FE không sửa).

## 1. Vai trò & quyền ghi

| Vai trò | Công cụ | Được ghi | Không được |
|---|---|---|---|
| Orchestrator (lead) | session chính: read/grep/glob + `subagent` + `question` | plan, `docs/**`, `WORKFLOW_STATE.md`, contract draft | source code FE/BE; tự approve plan; tự commit/push |
| FE agent | `subagent(agent: general)` | `FE_arbitrage/**` | contract, BE, ngoài allowlist |
| BE agent | `subagent(agent: general)` | `go_be_arbitrage/**` | FE; đổi contract khi chưa được human duyệt |
| Reviewer | `subagent(agent: code-reviewer)` | không (read-only) | sửa code |
| Audit | `subagent(agent: explore)` | không | sửa code |

- Lead được ghi plan/docs/state; **không** ghi source code. Nếu lead không có write tool trong session, dispatch docs-only subagent.
- Build phải thoát Plan mode trước khi dispatch.
- Mỗi agent làm trên branch riêng trong repo của mình:
  - BE: `feat/<slug>-be`; FE: `feat/<slug>-fe`.
  - Chỉ dùng `opencode.worktree_create` khi 2 batch **cùng repo** chạy đồng thời và file không giao nhau; sau đó `opencode.session.move`.

## 2. Batch schema (bắt buộc trong plan)

```yaml
batch: FE-1
owner: FE            # FE | BE | both
depends_on: []
risk: low            # low | high
allowlist:
  - FE_arbitrage/src/app/[locale]/(protected)/groups/**
  - FE_arbitrage/src/services/traders.ts
acceptance:
  - "Stats strip 4 metrics"
  - "Favorite toggle persists"
tests:
  - pnpm type-check
  - pnpm lint
  - pnpm vitest run tests/unit/groups
```

## 3. Gates (không bỏ)

1. Plan → **HUMAN GATE 1**.
2. Contract (nếu có API) → **HUMAN GATE 2** → **FREEZE**.
3. Build song song theo §4.
4. Review từng batch (`code-reviewer`, BLOCKER/NIT).
5. Integration + full e2e **một lần**.
6. DoD: BE tests, FE unit/e2e, type-check, lint xanh; docs cập nhật
   (`docs/TESTPLAN.md`, `docs/test-cases.md`, swagger nếu endpoint đổi).
7. **HUMAN GATE cuối**: hỏi "Bạn muốn tôi commit + push không?".

Không API work trước freeze. Không tự approve/commit.

## 4. Chạy song song / phải chờ

| Tình huống | Hành vi |
|---|---|
| Contract chưa freeze | Không làm API; việc độc lập (UI tĩnh, util, logic nội bộ) vẫn chạy |
| Contract đã freeze | FE (mock/fixtures) + BE chạy song song |
| FE cần endpoint BE chưa xong | FE tiếp với mock, gắn `waiting-on: BE:<batch>`, chuyển batch khác |
| Batch có `depends_on` chưa xong | Chỉ batch đó chờ |
| Cần đổi contract | Dừng phần liên quan → lead → human → cập nhật → báo 2 bên |
| Cần sửa ngoài allowlist | Dừng, báo lead |
| Test flaky | Retry tối đa 2 lần rồi báo lead |

## 5. Sync points

- **SP1**: contract freeze → bắt đầu build song song.
- **SP2**: BE xong endpoint FE cần → FE đổi mock → BE thật.
- **SP3**: Integration: merge FE + BE, chạy full e2e một lần.
- Lệch contract: BE sai → BE fix; FE sai → FE fix; lead phân xử theo contract.

## 6. Dispatch & verify (OpenCode)

- Mỗi batch = 1 subagent session. Lưu `sessionID` để follow-up fix (tối đa 2 vòng).
- Prompt phải có: repo path tuyệt đối, branch, allowlist, acceptance checklist, lệnh test, link contract, report format.
- **Verify sau mỗi dispatch**: file tồn tại + `git diff --stat` + test targeted. Subagent báo done nhưng file không có → coi như fail, chia nhỏ re-dispatch.
- Không dispatch 2 subagent ghi cùng repo cùng lúc (trừ worktree riêng).
- Reviewer chạy được song song batch kế tiếp nếu không phụ thuộc.

## 7. Review & test

- Mỗi batch: unit targeted + type-check + lint. Không full e2e trong batch.
- `code-reviewer`: BLOCKER fix ngay; NIT gom fix cuối. Tối đa 2 vòng/batch.
- Batch `risk: low` (thuần UI) gộp 2–3 batch rồi review một lần.
- Full unit + e2e + visual snapshot chỉ ở Integration; snapshot regenerate sau khi UI chốt.

## 8. Báo cáo & state

- Report: `Batch | Agent | Files changed | Tests (cmd → result) | Deviations | Open questions`.
- Lead cập nhật `docs/WORKFLOW_STATE.md` sau mỗi batch (lead ghi trực tiếp nếu có write tool; nếu không, dispatch docs-only subagent). Có thể gộp ở SP1/SP2/SP3.

### WORKFLOW_STATE.md (mẫu)

```markdown
# WORKFLOW STATE
Updated: 2026-11-24
| Batch | Owner | Status | Depends on | Blocker |
|---|---|---|---|---|
| BE-1 | BE | done | — | — |
| FE-1 | FE | doing | — | — |
| FE-2 | FE | todo | BE-2 | waiting BE-2 |
```

Status: `todo | doing | waiting-on | reviewing | done`.

## 9. Lệnh thật

BE (`go_be_arbitrage/`):

```bash
go vet ./...
golangci-lint run ./...
go test -short -race -count=1 ./internal/<pkg>/...
go test -tags=integration -race -count=1 ./internal/<pkg>/...
go test -tags=e2e -race -count=1 ./...
make check
```

FE (`FE_arbitrage/`):

```bash
pnpm type-check
pnpm lint
pnpm vitest run <path>
pnpm test
npx playwright test <spec>
pnpm test:e2e      # cần BE :8080 + FE :3000
```

## 10. An toàn

- Không secrets; `.env` gitignored, chỉ `.env.example` tracked.
- Không sửa test để cho xanh; chỉ khi behavior đổi có chủ đích, ghi lý do.

## 11. Lỗi hay gặp

> Trống. Sau mỗi review thêm: lỗi → cách tránh → batch.
> Ví dụ: "Thiếu i18n key en/vi → checklist trước dispatch".
