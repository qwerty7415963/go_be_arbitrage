//go:build integration

package trader

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeFills struct {
	fills     []Fill
	truncated bool
	err       error
	block     bool
	calls     int
}

func (f *fakeFills) FetchTraderFills(ctx context.Context, _ string, _, _ int64) ([]Fill, bool, error) {
	f.calls++
	if f.block {
		<-ctx.Done()
		return nil, false, ctx.Err()
	}
	return f.fills, f.truncated, f.err
}

func syncFixture(t *testing.T, fills *fakeFills) (*Repository, *SyncService, string) {
	t.Helper()
	pool := testPool(t)
	t.Cleanup(pool.Close)
	repo := NewRepository(pool)
	venueID := testVenue(t, pool)
	addr := testAddr()
	cleanupAddrs(t, pool, venueID, addr)
	if _, _, err := repo.UpsertRegistry(t.Context(), venueID, addr,
		SourceLeaderboard, nil, nil); err != nil {
		t.Fatalf("registry: %v", err)
	}
	svc := NewSyncService(repo, fills, venueID, DefaultSyncOptions())
	return repo, svc, addr
}

func dayFills(now time.Time) []Fill {
	d1 := now.Add(-48 * time.Hour).Truncate(24 * time.Hour)
	d2 := now.Add(-24 * time.Hour).Truncate(24 * time.Hour)
	return []Fill{
		{Market: "BTC", Tid: 1, FilledAt: d1.Add(2 * time.Hour), Buy: true, Quantity: 1, Price: 100, Fee: 0.1},
		{Market: "BTC", Tid: 2, FilledAt: d1.Add(3 * time.Hour), Buy: false, Quantity: 1, Price: 110, ClosedPnL: 10, Fee: 0.1},
		{Market: "ETH", Tid: 3, FilledAt: d2.Add(2 * time.Hour), Buy: false, Quantity: 1, Price: 50, Fee: 0.1},
		{Market: "ETH", Tid: 4, FilledAt: d2.Add(3 * time.Hour), Buy: true, Quantity: 1, Price: 55, ClosedPnL: -5, Fee: 0.1},
	}
}

// RET-I-01: buffer older than retention is purged per sync; recent kept.
func TestSync_BufferPurge(t *testing.T) {
	now := time.Now().UTC()
	repo, svc, addr := syncFixture(t, &fakeFills{})
	ctx := context.Background()
	venueID, _ := repo.VenueIDByCode(ctx, testVenueCode)

	oldFill := Fill{Market: "BTC", Tid: 1, FilledAt: now.Add(-70 * 24 * time.Hour),
		Buy: true, Quantity: 1, Price: 100}
	newFill := Fill{Market: "BTC", Tid: 2, FilledAt: now.Add(-time.Hour),
		Buy: true, Quantity: 1, Price: 100}
	if _, err := repo.UpsertBufferFills(ctx, venueID, addr, []Fill{oldFill, newFill}); err != nil {
		t.Fatalf("stage: %v", err)
	}
	if err := svc.SyncWallet(ctx, addr, now); err != nil {
		t.Fatalf("sync: %v", err)
	}
	kept, err := repo.LoadBufferFills(ctx, venueID, addr, now.Add(-400*24*time.Hour), now.Add(time.Hour))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(kept) != 1 || kept[0].Tid != 2 {
		t.Errorf("only the recent fill must survive: %+v", kept)
	}
}

// SYNC-I-01: full pipeline seed → sync → daily → period rows.
func TestSync_FullPipeline(t *testing.T) {
	now := time.Now().UTC()
	d1 := now.Add(-48 * time.Hour).Truncate(24 * time.Hour)
	d2 := now.Add(-24 * time.Hour).Truncate(24 * time.Hour)
	repo, svc, addr := syncFixture(t, &fakeFills{fills: dayFills(now)})
	ctx := context.Background()
	venueID, _ := repo.VenueIDByCode(ctx, testVenueCode)

	if err := svc.SyncWallet(ctx, addr, now); err != nil {
		t.Fatalf("sync: %v", err)
	}
	d1row, _ := repo.GetDailyStats(ctx, venueID, addr, d1)
	if d1row == nil || d1row.TradeCount != 1 || d1row.WinCount != 1 {
		t.Fatalf("day1: %+v", d1row)
	}
	m, err := repo.GetPeriodMetrics(ctx, venueID, addr, Period30D)
	if err != nil || m == nil {
		t.Fatalf("period: %v", err)
	}
	if m.TradeCount == nil || *m.TradeCount != 2 {
		t.Errorf("trades: %+v", m.TradeCount)
	}
	if m.WinRate == nil || *m.WinRate != 50.0 {
		t.Errorf("win rate 1/1: %+v", m.WinRate)
	}
	if m.DataStatus != DataReady || m.CalculationVersion != CurrentCalculationVersion {
		t.Errorf("status/version: %+v", m)
	}
	if m.ROI == nil {
		t.Error("fallback ROI must be computed without LB ref")
	}
	st, _ := repo.GetSyncState(ctx, venueID, addr)
	if st.SyncStatus != "ready" || st.BackfillCompletedAt == nil || st.FillsLastTime == nil {
		t.Errorf("sync state: %+v", st)
	}

	// BE-011: same window twice → identical rows, no duplicates.
	before, _ := repo.GetDailyStats(ctx, venueID, addr, d2)
	if err := svc.SyncWallet(ctx, addr, now); err != nil {
		t.Fatalf("resync: %v", err)
	}
	after, _ := repo.GetDailyStats(ctx, venueID, addr, d2)
	if before.TradeCount != after.TradeCount || *before.RealizedPnL != *after.RealizedPnL {
		t.Errorf("rerun changed rows: %+v -> %+v", before, after)
	}
	m2, _ := repo.GetPeriodMetrics(ctx, venueID, addr, Period30D)
	if *m2.TradeCount != 2 {
		t.Errorf("rerun duplicated trades: %+v", m2.TradeCount)
	}
}

// SYNC-I-02: crash between data commit and cursor advance → reprocess safe.
func TestSync_CrashRecovery(t *testing.T) {
	now := time.Now().UTC()
	repo, svc, addr := syncFixture(t, &fakeFills{fills: dayFills(now)})
	ctx := context.Background()
	venueID, _ := repo.VenueIDByCode(ctx, testVenueCode)

	if err := svc.SyncWallet(ctx, addr, now); err != nil {
		t.Fatalf("sync: %v", err)
	}
	// Simulate the crash: cursor rolled back, data committed.
	old := now.Add(-30 * 24 * time.Hour)
	if err := repo.UpdateSyncProgress(ctx, venueID, addr, &old, nil, "syncing", nil, 0); err != nil {
		t.Fatalf("rewind: %v", err)
	}
	if err := svc.SyncWallet(ctx, addr, now); err != nil {
		t.Fatalf("recovery: %v", err)
	}
	m, _ := repo.GetPeriodMetrics(ctx, venueID, addr, Period30D)
	if m.TradeCount == nil || *m.TradeCount != 2 {
		t.Errorf("double count after crash: %+v", m.TradeCount)
	}
}

// SYNC-I-03 (BE-010): fetch failures record backoff state; the scheduler
// skips cooling-down wallets and retries them later.
func TestSync_BackoffAndRetry(t *testing.T) {
	now := time.Now().UTC()
	fetch := &fakeFills{err: errors.New("upstream 429")}
	repo, svc, addr := syncFixture(t, fetch)
	ctx := context.Background()
	venueID, _ := repo.VenueIDByCode(ctx, testVenueCode)

	if err := svc.SyncWallet(ctx, addr, now); err == nil {
		t.Fatal("expected fetch error")
	}
	st, _ := repo.GetSyncState(ctx, venueID, addr)
	if st.SyncStatus != "error" || st.RetryCount != 1 || st.LastError == nil {
		t.Errorf("error state: %+v", st)
	}
	m, _ := repo.GetPeriodMetrics(ctx, venueID, addr, Period30D)
	if m == nil || m.DataStatus != DataError {
		t.Errorf("period status must flip to error: %+v", m)
	}

	done, failed := svc.SyncAll(ctx, now)
	if done != 0 || failed != 0 {
		t.Errorf("cooling-down wallet must be skipped: done=%d failed=%d", done, failed)
	}

	// Later cycle: backoff expired, fetch works → ready, retry reset.
	_, err := repo.pool.Exec(ctx,
		`UPDATE trader_sync_state SET updated_at = NOW() - INTERVAL '1 hour'
		 WHERE venue_id = $1 AND wallet_address = $2`, venueID, addr)
	if err != nil {
		t.Fatalf("time travel: %v", err)
	}
	fetch.err = nil
	fetch.fills = dayFills(now)
	done, failed = svc.SyncAll(ctx, now)
	if done != 1 || failed != 0 {
		t.Fatalf("retry: done=%d failed=%d", done, failed)
	}
	st, _ = repo.GetSyncState(ctx, venueID, addr)
	if st.SyncStatus != "ready" || st.RetryCount != 0 || st.LastError != nil {
		t.Errorf("recovered state: %+v", st)
	}
}

// SYNC-I-04 (BE-032): hung upstream hits the wallet timeout; state stays
// recoverable.
func TestSync_UpstreamTimeout(t *testing.T) {
	now := time.Now().UTC()
	repo, svc, addr := syncFixture(t, &fakeFills{block: true})
	svc.opts.WalletTimeout = 200 * time.Millisecond
	ctx := context.Background()
	venueID, _ := repo.VenueIDByCode(ctx, testVenueCode)

	if err := svc.SyncWallet(ctx, addr, now); err == nil {
		t.Fatal("expected timeout error")
	}
	st, _ := repo.GetSyncState(ctx, venueID, addr)
	if st.SyncStatus != "error" {
		t.Errorf("timeout state: %+v", st)
	}
}

// ROI/PnL passthrough (spec D4 + §9): fresh LB ref wins for display;
// realized_pnl always carries the computed sum for reconciliation.
func TestSync_ROIPassthrough(t *testing.T) {
	now := time.Now().UTC()
	repo, svc, addr := syncFixture(t, &fakeFills{fills: dayFills(now)})
	ctx := context.Background()
	venueID, _ := repo.VenueIDByCode(ctx, testVenueCode)

	lbROI, lbPnL := 0.05, 42000.0
	if err := repo.UpsertLeaderboardRef(ctx, &LeaderboardRef{VenueID: venueID,
		WalletAddress: addr, Window: "month", ROI: &lbROI, PnL: &lbPnL, FetchedAt: now}); err != nil {
		t.Fatalf("ref: %v", err)
	}
	if err := svc.SyncWallet(ctx, addr, now); err != nil {
		t.Fatalf("sync: %v", err)
	}
	m, _ := repo.GetPeriodMetrics(ctx, venueID, addr, Period30D)
	if m.ROI == nil || *m.ROI != 5.0 {
		t.Errorf("fresh LB ROI passthrough: %+v", m.ROI)
	}
	if m.PnL == nil || *m.PnL != lbPnL {
		t.Errorf("fresh LB PnL passthrough: %+v", m.PnL)
	}
	if m.RealizedPnL == nil || *m.RealizedPnL < 4.59 || *m.RealizedPnL > 4.61 {
		t.Errorf("realized must stay computed (9.8-5.2): %+v", m.RealizedPnL)
	}
}

// BE-037: lastSuccess older than the threshold recalculates as stale
// (readers apply the same rule at serve time; writers preserve vintage).
func TestSync_StaleRule(t *testing.T) {
	repo, svc, addr := syncFixture(t, &fakeFills{})
	ctx := context.Background()
	old := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	if err := svc.recalcAll(ctx, addr, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		false, &old, false); err != nil {
		t.Fatalf("recalc: %v", err)
	}
	venueID, _ := repo.VenueIDByCode(ctx, testVenueCode)
	m, _ := repo.GetPeriodMetrics(ctx, venueID, addr, Period30D)
	if m == nil || m.DataStatus != DataStale {
		t.Errorf("want stale: %+v", m)
	}
	if m.AsOf.IsZero() || !m.AsOf.Equal(old) {
		t.Errorf("vintage preserved: %v", m.AsOf)
	}
}

// SYNC-I-04: pool end-to-end — 20 wallets across 4 workers, all done once.
func TestSync_PoolEndToEnd(t *testing.T) {
	now := time.Now().UTC()
	ctx := context.Background()
	pool := testPool(t)
	t.Cleanup(pool.Close)
	repo := NewRepository(pool)
	venueID := testVenue(t, pool)

	addrs := make([]string, 20)
	for i := range addrs {
		addrs[i] = testAddr()
		if _, _, err := repo.UpsertRegistry(ctx, venueID, addrs[i],
			SourceLeaderboard, nil, nil); err != nil {
			t.Fatalf("registry: %v", err)
		}
	}
	t.Cleanup(func() {
		for _, a := range addrs {
			_, _ = pool.Exec(context.Background(),
				`DELETE FROM trader_registry WHERE venue_id = $1 AND wallet_address = $2`, venueID, a)
		}
	})
	opts := DefaultSyncOptions()
	opts.Workers = 4
	svc := NewSyncService(repo, &fakeFills{}, venueID, opts)

	done, failed := svc.SyncAll(ctx, now)
	if done != 20 || failed != 0 {
		t.Errorf("pool: done=%d failed=%d", done, failed)
	}
	for _, a := range addrs {
		st, err := repo.GetSyncState(ctx, venueID, a)
		if err != nil || st == nil || st.BackfillCompletedAt == nil {
			t.Errorf("%s not completed: %+v %v", a, st, err)
		}
	}
}

// SYNC-I-06: cold wallets wait out ColdInterval; hot wallets run every tick.
func TestSync_ColdTierSkip(t *testing.T) {
	now := time.Now().UTC()
	ctx := context.Background()
	pool := testPool(t)
	t.Cleanup(pool.Close)
	repo := NewRepository(pool)
	venueID := testVenue(t, pool)

	cold := testAddr()
	tradeAt := now.Add(-60 * 24 * time.Hour)
	if _, _, err := repo.UpsertRegistry(ctx, venueID, cold,
		SourceLeaderboard, nil, &tradeAt); err != nil {
		t.Fatalf("registry: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM trader_registry WHERE venue_id = $1 AND wallet_address = $2`, venueID, cold)
	})
	opts := DefaultSyncOptions()
	svc := NewSyncService(repo, &fakeFills{}, venueID, opts)
	// First pass completes the backfill (pending always due).
	if done, _ := svc.SyncAll(ctx, now); done != 1 {
		t.Fatalf("first pass: done=%d", done)
	}
	// Second pass immediately after: cold wallet must wait out 24h.
	if done, failed := svc.SyncAll(ctx, now); done != 0 || failed != 0 {
		t.Errorf("cold skip: done=%d failed=%d", done, failed)
	}
}
