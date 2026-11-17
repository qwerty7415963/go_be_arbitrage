# Contract v2.2 — Positions TP/SL

## 0. Scope & compatibility
- Additive only to GET /api/v1/traders/{wallet}/positions
- No migration, no new endpoint, no auth change
- Old clients ignore new fields

## 1. Endpoint
GET /api/v1/traders/{wallet}/positions?venue=hyperliquid&sort=coin&dir=asc
Response PositionSnapshotDTO with positions[] now including tp_pxs, sl_pxs.

## 2. Fields
- tp_pxs: number[] (always present, [] when none)
- sl_pxs: number[] (always present, [] when none)
- JSON keys snake_case; no omitempty.

## 3. Semantics
Source: HL frontendOpenOrders (new method FetchFrontendOpenOrders in internal/hyperliquid/tabs.go and live.go). Fetched per dex within the same 60s cache fill (cache key positions-live|<venue>|<addr>).
Valid order:
- reduce_only == true
- trigger_px parseable and > 0
- side is closing side: LONG position → order side "A" (sell); SHORT → "B" (buy) (isClosingSide)
- coin matches position coin: exact uppercase, fallback suffix after last ':' (normCoin)
- classification by orderType (case-insensitive): contains "take profit" → TP; contains "stop" (and not TP) → SL; otherwise skipped (isTP/isSL)
- bracket children flattened recursively; child inherits parent coin/side/reduceOnly/triggerPx/orderType/isTrigger/isPositionTpsl when missing (flattenOrders)
- dedupe by canonical decimal string (dedupeFloats)
- sort by |px - mark_price| asc; mark null → price asc; tie → price asc (sortTPSL)
Errors: per-dex frontend fetch failure logged (slog.Warn) and skipped; positions still data_status=ready; all fail → empty arrays. Stale cache on positions fetch failure retains tp/sl.
No storage.

## 4. BE implementation map
- internal/hyperliquid/tabs.go: OpenOrder + IsTrigger, Children; FetchFrontendOpenOrders
- internal/hyperliquid/live.go: LiveClient.FetchFrontendOpenOrders
- internal/trader/positions.go: Position.TpPxs/SlPxs; attachTPSL; flattenOrders; normCoin; parseTrigger; isTP; isSL; isClosingSide; dedupeFloats; sortTPSL
- internal/trader/service.go: optional tpslFetcher interface; fetch per dex in errgroup; snapshotToDTO copies arrays, non-nil
- internal/trader/ondemand.go: LiveClient interface unchanged (optional cast)

## 5. FE contract
- types/trader.ts: tp_pxs?, sl_pxs?
- positions-section.tsx: columns TP, SL after Liquidation Price; plain headers; cell text max 2 values + "+N", title full list; '—' when empty
- formatTpSlList in trader-format.ts
- i18n keys traders.tp, traders.sl

## 6. Tests
Reference docs/TESTPLAN.md §19.27 for matrix (TPSL-01..TPSL-07 + helpers).

## 7. Verification
gofmt, go vet, go test -short -race, go test -tags=e2e, go build, swag init idempotent.
