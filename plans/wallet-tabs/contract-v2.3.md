# Contract v2.3 — Event-driven position push via WebSocket

## 0. Scope & compatibility
- Additive only: upgrades `wallet.position.updated` payload; no migration, no REST endpoint change.
- Old FE clients ignore the new payload (validator requires positions[] + data_status; old payload `{as_of}` is rejected → no patch, safe).
- Rollback: revert BE emit to `{as_of}` payload.

## 1. Event
`wallet.position.updated` over GET /traders/ws?wallet= (existing transport). Envelope: `{type, data, state}` (WalletEvent).

## 2. Payload (upgraded)
data = full PositionSnapshotDTO:
{
  summary: {account_value, total_ntl_pos, total_margin_used, as_of} | null,
  positions: PositionDTO[] (with tp_pxs, sl_pxs),
  data_status: "ready" | "error",
  as_of: ISO string | null
}
Same shape as GET /api/v1/traders/{wallet}/positions response data (minus sort/dir; positions sorted coin asc).

## 3. Trigger rules (BE)
- Event-driven: schedulePositionRefresh on:
  - wallet.fill.created (OnFill / OnRawUserFill)
  - wallet.order.created / wallet.order.updated (OnOrder)
- NOT on funding (OnFunding does not trigger position refresh).
- Debounce 2s per wallet (coalesce bursts).
- Also on bootstrap (first subscribe) and reconnect/resync: emit full snapshot.
- Periodic REST poll 30s remains as fallback (FE side).

## 4. BE implementation
- WatcherManager: PositionSnapshotProvider seam (WithPositionProvider), position timer + pending flag + last DTO.
- BuildLivePositionsSnapshot(ctx, client, wallet, dexes) in positions.go: fetch clearinghouseState (multi-dex) + frontendOpenOrders (TP/SL attach). Exported.
- SnapshotToPositionDTO exported wrapper.
- app.go wiring provider using hlLive + appSettingsService.GetPerpDexes.
- Teardown stops timer.

## 5. Failure behavior
- Provider error: no emit; keep last snapshot; log warn + counter; REST fallback covers.
- Gate full: skip + pending + retry 2s; counter dropped.
- TP/SL orders fetch failure: preserve last TP/SL per coin if available else [].
- Multi-dex partial failure: emit snapshot of successful dexes, data_status ready.

## 6. FE
- use-trader-positions: onPosition patches react-query cache with client-side resort (sort/dir).
- Keep REST poll 30s fallback.
- position-sort.ts mirror BE comparator (nulls last, tiebreak coin asc).

## 7. Tests
BE: TestWatcher_PositionRefresh_Debounce, TestWatcher_PositionRefresh_GateFullSkip, TestWatcher_PositionRefresh_ErrorKeepsLast, TestWatcher_PositionUpdated_BootstrapFullSnapshot, TestWatcher_Teardown_StopsPositionTimer, TestBuildLivePositionsSnapshot_AttachesTPSL, WS e2e.
FE: position-sort tests, use-trader-positions cache patch tests.
TESTPLAN §19.29, docs/test-cases.md.

## 8. Verification
gofmt, go vet, go test -short -race, go test -tags=e2e, go build, swag init idempotent; FE type-check/lint/format/test/build/playwright.
