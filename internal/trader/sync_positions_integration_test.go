//go:build integration

package trader

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakePositions struct {
	snap  *PositionSnapshot
	err   error
	calls int
}

func (f *fakePositions) FetchPositions(_ context.Context, _ string) (*PositionSnapshot, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.snap, nil
}

func posSnap(coin string) *PositionSnapshot {
	return &PositionSnapshot{
		Positions:       []Position{{Coin: coin, Side: "LONG", Size: 1}},
		AccountValue:    fptr(1000),
		TotalNtlPos:     fptr(500),
		TotalMarginUsed: fptr(100),
		AsOf:            time.Now().UTC(),
	}
}

// SYNC-I-07: positions fetch failure keeps last snapshot, pass still succeeds.
func TestSync_PositionsFailureKeepsSnapshot(t *testing.T) {
	now := time.Now().UTC()
	repo, svc, addr := syncFixture(t, &fakeFills{})
	ctx := context.Background()
	venueID, _ := repo.VenueIDByCode(ctx, testVenueCode)

	if err := repo.ReplacePositions(ctx, venueID, addr, posSnap("BTC")); err != nil {
		t.Fatalf("seed: %v", err)
	}
	svc.WithPositions(&fakePositions{err: errors.New("hl down")})
	if err := svc.SyncWallet(ctx, addr, now); err != nil {
		t.Fatalf("wallet pass must succeed on positions failure: %v", err)
	}
	got, err := repo.GetPositions(ctx, venueID, addr)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got) != 1 || got[0].Coin != "BTC" {
		t.Errorf("last snapshot must be kept: %+v", got)
	}
	st, _ := repo.GetSyncState(ctx, venueID, addr)
	if st.LastPositionsSyncAt != nil {
		t.Errorf("failed sync must not touch last_positions_sync_at: %+v", st.LastPositionsSyncAt)
	}
}

// SYNC-I-08: SyncWallet persists closed trades + positions + cursor.
func TestSync_WalletPersistsTradesAndPositions(t *testing.T) {
	now := time.Now().UTC()
	repo, svc, addr := syncFixture(t, &fakeFills{fills: dayFills(now)})
	ctx := context.Background()
	venueID, _ := repo.VenueIDByCode(ctx, testVenueCode)

	svc.WithPositions(&fakePositions{snap: posSnap("BTC")})
	if err := svc.SyncWallet(ctx, addr, now); err != nil {
		t.Fatalf("sync: %v", err)
	}
	rows, err := repo.ListTrades(ctx, venueID, addr, 10, nil, "", nil)
	if err != nil {
		t.Fatalf("list trades: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("want 2 durable closed trades, got %d: %+v", len(rows), rows)
	}
	poss, err := repo.GetPositions(ctx, venueID, addr)
	if err != nil || len(poss) != 1 || poss[0].Coin != "BTC" {
		t.Fatalf("positions: %+v err=%v", poss, err)
	}
	st, _ := repo.GetSyncState(ctx, venueID, addr)
	if st.LastPositionsSyncAt == nil {
		t.Error("last_positions_sync_at must be set on success")
	}
	// Old trades are pruned by the same pass (15d retention): seed an old
	// row, resync with no new fills, it must disappear.
	oldDay := now.Add(-20 * 24 * time.Hour).Truncate(24 * time.Hour)
	if err := repo.ReplaceTradesForDay(ctx, venueID, addr, oldDay, []CompletedTrade{
		{Market: "OLD", Long: true, OpenTime: oldDay.Add(time.Hour), CloseTime: oldDay.Add(2 * time.Hour), Volume: 1, PnL: 1, Fees: 0, Fills: 1},
	}); err != nil {
		t.Fatalf("seed old: %v", err)
	}
	svc2 := NewSyncService(repo, &fakeFills{}, venueID, DefaultSyncOptions()).
		WithPositions(&fakePositions{snap: posSnap("BTC")})
	if err := svc2.SyncWallet(ctx, addr, now.Add(time.Minute)); err != nil {
		t.Fatalf("resync: %v", err)
	}
	rows, _ = repo.ListTrades(ctx, venueID, addr, 10, nil, "", nil)
	for _, r := range rows {
		if r.Market == "OLD" {
			t.Errorf("OLD row must be pruned by SyncWallet: %+v", rows)
		}
	}
}
