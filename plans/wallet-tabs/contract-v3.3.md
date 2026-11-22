# Contract v3.3 — Đa ngôn ngữ EN/VI (BE API + FE UI)

## 0. Quyết định đã chốt

1. Number formatting: giữ en-US grouping (`1,234.56`); chỉ localize text/date/relative time.
2. Notification language: setting riêng (Telegram binding locale), default = UI language tại thời điểm link.
3. Error mapping: BE localize theo Accept-Language (Method B); FE fallback cho network/unknown.
4. Xoá dead namespaces `auth`, `wallets`.
5. Method B: BE tự dịch `error.message` theo ngôn ngữ request.

## 1. Locale negotiation (BE)

- Header `Accept-Language` (RFC 7231, q-values), hỗ trợ `vi`, `en`.
- Optional query `?lang=vi|en` override (testing/debug).
- Default: `vi` (khớp FE default).
- Middleware parse → `c.Set("locale", locale)`.
- Không hỗ trợ locale khác → fallback `vi`.
- FE gửi `Accept-Language` trên mọi request (api-client + RSC fetch).
- CORS `Access-Control-Allow-Headers` thêm `Accept-Language`.

## 2. Message catalog (BE)

- Package `internal/i18n`:
  - `//go:embed messages/*.json`; files `messages/en.json`, `messages/vi.json`.
  - Keys: error codes (`AUTH-004`), validation keys (`validation.required`, `validation.email`, `validation.min`, `validation.max`, `validation.invalid`, `validation.datetime`, `validation.limit`, `validation.range`, `validation.duplicate`), field labels (`field.email`, `field.password`, ...).
  - Placeholder syntax `{name}`; params `map[string]any`.
  - `Translate(locale, key string, params map[string]any, fallback string) string`.
  - Fallback chain: locale → `en` → fallback (English domain message) → key.
- Catalog parity test: identical key sets EN/VI, no empty values, placeholders match.

## 3. Error response (BE)

- `domain.AppError` giữ `Code` + English `Message` (log/debug).
- Thêm optional `MessageKey string` + `Params map[string]any` vào `AppError`.
- Constructor mới: `domain.NewErrorKey(code, messageKey string, params map[string]any, fallbackMessage string)`; `NewError` cũ giữ nguyên.
- `api.RespondError`:
  - locale từ context.
  - key := appErr.MessageKey nếu có, else appErr.Code.
  - message := i18n.Translate(locale, key, params, appErr.Message).
  - response `{code, message, request_id}`; message localized.
  - English fallback chỉ log server-side (kèm request_id), không trả về khi đã có translation.
- Field errors: `field` giữ machine name; `message` localized qua `validation.*`; `code` stable (`required|email|min|max|invalid`).
- Ad-hoc codes (`VALIDATION_ERROR`, `ORDERBOOK-301`, etc.) được thêm key vào catalog hoặc map về `COMMON-901/902`.
- Cấm trả raw `err.Error()` làm `message`; dùng `COMMON-901` + log.

## 4. Dynamic messages

- Các message có giá trị động dùng `MessageKey` + `Params`:
  - range: `validation.range` {field,min,max}
  - invalid date: `validation.invalid_date` {field,value}
  - limit: `validation.limit` {min,max}
  - duplicate: `validation.duplicate` {field}
- Static errors dùng code làm key.

## 5. Telegram (BE)

- `telegram_bindings.locale` là source of truth cho notification.
- Endpoint mới `PATCH /v1/telegram/settings` (JWT):
  - body `{ "locale": "vi"|"en" }`
  - 200 `{ "locale": "..." }`; 400 invalid locale; 404 chưa link.
- Link flow: locale mặc định = UI language tại thời điểm link (hiện có).
- Bot replies localized theo binding locale; fallback link-code locale; default `vi`.
- Notification templates vi/en (đã có) dùng dispatcher hiện tại.
- Message đã enqueue không đổi ngôn ngữ khi user đổi locale (document).

## 6. FE

- Language switcher ở header: `LanguagesIcon` + code `VI`/`EN`; dropdown native names `Tiếng Việt`, `English`.
  - Chọn → `router.replace(pathname, { locale })` (từ `@/i18n/navigation`), giữ query.
  - Set cookie `NEXT_LOCALE` (next-intl `localeCookie`, maxAge 1 năm).
  - Nếu đã link Telegram → gọi `PATCH /v1/telegram/settings` (optional sync, default = UI language).
- `api-client.ts`: gửi `Accept-Language: <locale>` mọi request (đọc `document.documentElement.lang` hoặc cookie).
- RSC fetch: gửi `Accept-Language` từ page locale.
- Hiển thị `error.message` (BE localized). Giữ fallback keys: `errors.network`, `errors.timeout`, `errors.unknown`, `errors.aborted`.
- Bỏ per-code mapping (trừ fallback offline).
- Hoàn thiện dịch:
  - Namespace mới `fundingArbitrage` (~30 keys).
  - Fix 9 giá trị VI bị lỗi encoding; dịch 24 chuỗi English còn sót; thêm dấu 127 chuỗi VI không dấu.
  - ICU `positionsContext` đúng cả 2 locale.
  - Xoá dead namespaces `auth`, `wallets`.
  - Market detail headers, pagination aria-labels, dialog/sheet "Close", date picker months/weekdays/aria.
- Formatting: giữ en-US number grouping; date dùng `Intl.DateTimeFormat(locale)`; relative time qua i18n keys (ICU); date picker localized.
- Metadata: `generateMetadata` localized (title/description); `html lang` server-side; `setRequestLocale`.
- Tests FE: parity (keys, no U+FFFD, no empty), switcher (router/cookie/URL), api-client Accept-Language, funding VI render, formatting locale, E2E switch vi↔en (all pages, metadata, error message language).

## 7. API docs

- Swagger: mô tả `Accept-Language` (global description + endpoint optional param), error example ghi rõ message localized.
- README section i18n.

## 8. Test matrix (9 dimensions)

| ID | Chiều | Kịch bản | Input | Expected | Layer | Test name | File |
|---|---|---|---|---|---|---|---|
| I18N-01 | Happy | BE trả VI cho AUTH-004 | Accept-Language: vi, login duplicate | message "Email đã được đăng ký" | Handler | TestHandler_Login_DuplicateVI | internal/auth/handler_test.go |
| I18N-02 | Happy | BE trả EN cho AUTH-004 | Accept-Language: en | "Email already registered" | Handler | TestHandler_Login_DuplicateEN | same |
| I18N-03 | Happy | FE switch VI→EN | click EN | URL /en/..., text EN, cookie NEXT_LOCALE=en | Unit+E2E | language-switcher.test.tsx / i18n-switch.spec.ts | FE tests |
| I18N-04 | Happy | Funding page VI | /vi/funding-arbitrage | không còn English hardcoded | Unit | funding-arbitrage-i18n.test.tsx | FE |
| I18N-05 | Happy | Metadata localized | /vi, /en | title/description đúng locale | E2E | i18n-switch.spec.ts | FE |
| I18N-06 | Invalid | Accept-Language không hỗ trợ | fr-FR | fallback vi | Unit | TestParseAcceptLanguage_Unsupported | internal/i18n |
| I18N-07 | Invalid | PATCH locale sai | locale=fr | 400 INVALID_FILTER/localized | Handler | TestHandler_TelegramSettings_InvalidLocale | internal/telegram |
| I18N-08 | Invalid | Gin binding error localized | login thiếu email | field message localized, code INVALID | Handler | TestHandler_Login_BindingVI | internal/auth |
| I18N-09 | Boundary | q-values | `en-US,en;q=0.9,vi;q=0.8` | en | Unit | TestParseAcceptLanguage_QValues | internal/i18n |
| I18N-10 | Boundary | vi;q=0 bị bỏ | `vi;q=0,en;q=0.5` | en | Unit | TestParseAcceptLanguage_ZeroQ | same |
| I18N-11 | Boundary | header rỗng/không có | "" | vi | Unit | TestParseAcceptLanguage_Empty | same |
| I18N-12 | Boundary | code không có trong catalog | unknown code | fallback English message | Unit | TestTranslate_MissingKeyFallback | same |
| I18N-13 | Concurrency | 2 request vi/en đồng thời | 100 goroutine | message đúng locale từng request, no race | Unit (race) | TestConcurrentLocales | internal/i18n |
| I18N-14 | Recovery | catalog thiếu key VI | vi, key chỉ có EN | fallback EN | Unit | TestTranslate_LocaleFallbackEN | same |
| I18N-15 | Recovery | FE BE down | network error | errors.network localized | Unit | api-client-network-error.test.ts | FE |
| I18N-16 | Idempotency | cùng request 2 lần | vi, AUTH-004 | cùng message | Unit | TestTranslate_Idempotent | internal/i18n |
| I18N-17 | Idempotency | PATCH locale 2 lần | vi→vi | 200, binding locale vi | Integration | TestRepo_UpdateLocale_Idempotent | internal/telegram |
| I18N-18 | Staleness | message enqueue trước khi đổi locale | en queue, đổi vi | message cũ giữ EN, message mới VI | Integration | TestNotify_LocaleChangeNotRetroactive | internal/notify |
| I18N-19 | Partial | một số code chưa dịch | mixed keys | code có translation dùng VI, còn lại fallback EN | Unit | TestTranslate_PartialCatalog | internal/i18n |
| I18N-20 | Security | raw err.Error không leak | DB error | response COMMON-901 localized, không có raw | Handler | TestHandler_RespondError_NoRawLeak | internal/api |
| I18N-21 | Security | params không inject HTML | field `<script>` | JSON escape, không render HTML | Unit | TestTranslate_ParamsEscaped | internal/i18n |
| I18N-22 | Failure | catalog file thiếu lúc start | missing embed | fail fast / log fatal | Unit | TestCatalog_LoadFailFast | internal/i18n |
| I18N-23 | Replay | cùng locale+code+params | 2 lần | output deterministic | Unit | TestTranslate_Deterministic | internal/i18n |
| I18N-24 | Failure | Accept-Language quá dài/malformed | 10KB header | fallback vi, không panic | Unit | TestParseAcceptLanguage_Malformed | internal/i18n |
| I18N-25 | Happy | Bot reply VI/EN | binding vi/en, /status | reply đúng ngôn ngữ | Unit | TestBot_Status_Locale | internal/telegram |
| I18N-26 | Happy | CORS allow Accept-Language | OPTIONS | header có Accept-Language | Handler | TestCORS_AllowAcceptLanguage | internal/httpserver |
| I18N-27 | Happy | FE api-client gửi header | locale vi | Accept-Language: vi | Unit | api-client-accept-language.test.ts | FE |
| I18N-28 | Happy | Formatting VI | date/number | number en-US, date vi-VN | Unit | format-locale.test.ts | FE |
| I18N-29 | Happy | Parity catalogs | EN/VI | cùng key set, không empty, placeholder khớp | Unit | TestCatalogParity | internal/i18n |
| I18N-30 | Happy | FE message parity | en/vi json | 0 missing, 0 U+FFFD | Unit | messages-parity.test.ts | FE |

## 9. Phases

1. BE i18n core (package, catalogs, middleware, RespondError, field errors) + tests.
2. BE Telegram settings endpoint + bot replies + CORS + tests.
3. FE switcher + Accept-Language + fallback + funding namespace + corrupt fixes + formatting + metadata + tests.
4. Docs: docs/i18n.md, TESTPLAN §19.34, docs/test-cases.md.
5. Verify: switch EN/VI all pages, error language, Telegram messages, metadata.

## 10. Verification commands

BE: gofmt, go vet, go test -short -race ./internal/i18n/... ./internal/api/... ./internal/auth/... ./internal/telegram/... ./internal/httpserver/..., integration/e2e with -p 1, go build, swag idempotent.

FE: pnpm type-check, lint, format:check, test, build, playwright (i18n-switch + existing specs).
