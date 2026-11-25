# Session Policy (OpenCode) — v2.1

## 1. Sessions per role
- Một session sống suốt feature cho mỗi role: `FE-impl`, `BE-impl`, `QA`.
- Lưu `sessionID` vào `plans/<feature>/sessions.md`.
- Batch sau = follow-up bằng `sessionID` thật, không tạo session mới.

## 2. Concurrency
- Tối đa 2 subagent chạy song song: FE + BE.
- QA chạy sau khi FE/BE xong.
- Risk reviewer (`code-reviewer`) chỉ chạy khi QA flag concurrency/money/migrations/auth.

## 3. Dispatch rules
- Dispatch mới: **không truyền `sessionID`**.
- Follow-up: truyền đúng `ses_...` từ `sessions.md`.
- Prompt ngắn, tham chiếu file contract/queue; không paste nội dung dài.
- Coalesce nhiều file/thay đổi liên quan vào một dispatch.
- Verify sau dispatch: file tồn tại + `git diff --stat` + test targeted.
- Tool trả "Subagent session not found" → coi như chưa chạy, dispatch lại đúng cách.

## 4. Rate limit (429/502)
- Dừng dispatch mới.
- Backoff, retry tối đa 2.
- Vẫn lỗi → đánh dấu `BLOCKED: rate-limit` trong queue, báo human.
- Không tạo thêm session để "thử lại".

## 5. Khi nào tạo session mới
- Session hỏng context (lặp lỗi, trả lời sai) → thay tối đa 1 lần/role/feature, ghi lý do vào `sessions.md`.
- Đổi contract version lớn → reset FE/BE session.
- Feature mới → session mới.

## 6. Orchestrator writes docs
- Plan, contract, mock spec, checkpoint, queue, sessions: orchestrator viết trực tiếp (không subagent).
- Chỉ implementation/QA mới dùng subagent.

## 7. Templates
- `plans/<feature>/sessions.md`: bảng role | sessionID | status | last batch.
- `plans/<feature>/queue.md`: bảng batch | owner | status | depends_on | files.
