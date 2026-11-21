# Contract v3.2 — Remove /api prefix (keep /v1)

## 0. Scope
- Breaking change: remove `/api` prefix from all BE routes. Serve at `/v1/...` instead of `/api/v1/...`.
- No backward compat: `/api/v1/...` will return 404.
- FE proxy updated: `/api/:path*` → `${BACKEND_URL}/:path*` (no /api in destination).
- WS URL: `wss://api.hyperscanai.xyz/v1/traders/ws?...` (no /api).
- No new endpoints, no shape changes, no auth changes.
- Only route prefix change + reference updates.

## 1. Route changes
- `internal/httpserver/routes.go`: `v1 := s.engine.Group("/api/v1")` → `v1 := s.engine.Group("/v1")`
- All @Router annotations: `/api/v1/...` → `/v1/...`
- `/health`, `/ready`, `/ping` stay at root (unchanged).

## 2. BE reference updates
- e2e tests: `/api/v1/...` → `/v1/...`
- migrations: `000020_venue_endpoint_urls` references (if any absolute URLs)
- docs/swagger: auto-regen

## 3. FE reference updates
- `next.config.ts`: rewrites destination `${BACKEND_URL}/:path*` (remove /api)
- `src/lib/trader-activity-ws.ts`: buildActivityWsUrl → `${base}/v1/traders/ws?...`
- `src/services/traders.ts`: TRADERS_BASE → `/v1/traders` (if FE calls BE directly), TRADER_GROUPS_BASE → `/v1/trader-groups`
- Wait — FE calls via proxy `/api/v1/traders`. If proxy strips /api, FE still calls `/api/v1/traders` and BE receives `/v1/traders`. So FE service paths stay `/api/v1/...` (they go through proxy). Only the WS URL and next.config.ts rewrite change.
- Actually, let me reconsider: FE services use `TRADERS_BASE = '/api/v1/traders'` (relative). These go through Next.js rewrites. If rewrite is `/api/:path*` → `${BACKEND_URL}/:path*`, then `/api/v1/traders` → `BACKEND_URL/v1/traders`. That works.
- But if we keep FE service paths as `/api/v1/traders`, the WS URL must change to `/v1/traders/ws` (direct, not proxied).
- Tests: hardcoded `/api/v1/...` paths in FE tests must update to match new proxy behavior. Actually, FE tests mock services, not HTTP paths, so only e2e tests that hit real BE might need updating. But FE e2e tests hit the FE server which proxies, so `/api/v1/...` still works on FE side.

## 4. WS URL change
- `buildActivityWsUrl`: `${base}/v1/traders/ws?wallet=...` (was `/api/v1/traders/ws`)
- `NEXT_PUBLIC_WS_URL` default: `wss://api.hyperscanai.xyz` (base, no path) — FE constructs `${base}/v1/traders/ws`

## 5. No shape/auth/migration changes
- All response shapes unchanged.
- Auth unchanged.
- No new migration.
- Swagger paths change from /api/v1 to /v1.

## 6. Tests
BE: all e2e tests with /api/v1 paths must update to /v1. Unit/integration tests using httptest with /api/v1 paths must update.
FE: e2e tests hitting /api/v1 directly (if any) must update. Unit tests mock services, unaffected.

## 7. Verification
BE: gofmt, vet, build, unit, integration, e2e, swag init idempotent.
FE: type-check, lint, format, test, build, playwright.
