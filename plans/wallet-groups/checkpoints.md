# Wallet Groups — Checkpoints

## BE

### Impl checkpoint — go_be_arbitrage
- Feature: wallet-groups
- Contract version: v1.0
- Branch: dev
- Commit SHA: 55cf9e5 (later docs commits a5f0b33/f72a92d/b42498f/1891122)
- Files changed: migrations 000039/000040; internal/tradergroup/**; internal/trader/{equity,sync,fills,wallet_event_projector}.go; internal/storage/retention_service.go; internal/app/app.go; internal/domain/errors.go; docs/swagger.*
- Endpoints implemented: groups CRUD/favorite/duplicate/overview; members; stats; positions; performance; compare; activity; alerts CRUD/events/read/unread
- Analytics handlers/routes added: stats, positions, performance, compare
- Analytics handler tests: 9 added (invalid input, missing group, happy path per handler)
- Swagger: regenerated (docs/swagger.json/doc.go) for new analytics routes
- Tests run: go build ./... PASS; go vet ./... PASS; go test -short (tradergroup/trader/storage) PASS; go test -tags=integration (tradergroup/trader) PASS
- Evidence: migrations apply/down/up verified (version 40)
- Known risks: alert events handler type-asserts *Repository; activity filters in Go; alert evaluation N+1 (MVP)
- Open questions: none

## FE

### Impl checkpoint — FE_arbitrage

- Feature: wallet-groups (FE-1 overview, FE-2a detail tabs, FE-2b activity, FE-2c alerts, FE-2d compare)
- Contract version: v1.0
- Branch: dev (was feat/wallet-groups-fe, merged 03c8af9)
- Commit SHA: 03c8af9 (FE-1); e2e fixes 74413c9
- Files changed: groups overview page/components; groups/[groupId] page tabs (Overview/Members/Activity/Alerts); groups/compare page; types/services; messages en/vi; tests/e2e
- Screens: /groups overview; /groups/[groupId] Overview+Members+Activity+Alerts tabs (stats, positions, equity chart); /groups/compare compare page
- FE-2b: chart real SVG; Activity tab
- FE-2c: Alerts tab
- FE-2d: Compare page
- Tests run: pnpm type-check PASS; pnpm lint PASS; groups e2e 38/38 PASS; full e2e 154/155 (1 order-dependent flake)
- Evidence: SHA-256 mirrors for docs; git status
- Known risks: none open; FE-2 (2a/2b/2c/2d) complete
- Open questions: none

## QA

### QA checkpoint — wallet-groups
- Contract version: v1.0
- Conformance mock vs real BE: NOT RUN (harness pending)
- Gates:
  - mirror hash: PASS (supervisor docs; contract now byte-identical)
  - m-commit-gate: PASS (no stray staged files)
  - type-check: PASS
  - lint: PASS
  - format: N/A
  - unit: PASS (targeted BE/FE; 9 analytics handler tests)
  - build: PASS
  - e2e: groups 38/38 PASS; full 154/155 (1 order-dependent flake, passes isolated 3/3)
- Product: PASS for implemented scope (FE-1 + FE-2a/2b/2c/2d)
- Env: OK (BE 8080, FE 3000, Postgres 5433)
- Failed tests: header click sorts desc then asc (flake)
- Raw logs: temp logs
- Risk review files: alerts (money/concurrency) pending; projector (concurrency) pending


## BE

### Impl checkpoint - go_be_arbitrage (v1.1)
- Feature: wallet-groups
- Contract version: v1.1
- Branch: dev
- Files changed: internal/tradergroup/*, internal/trader/equity_test.go, docs/*
- Endpoints added: GET /v1/trader-groups/{id}/exposure
- Tests run: go build ./... PASS; go vet PASS; go test -short tradergroup/trader PASS
- Known risks: ROI uses transfer fetcher when configured; ledger failures make ROI null.
