//go:build integration

package trader

import (
	"context"
	"testing"
	"time"
)

func fptr(v float64) *float64 { return &v }

func snapWith(coins ...Position) *PositionSnapshot {
	return &PositionSnapshot{
		Positions:       coins,
		AccountValue:    fptr(10000),
		TotalNtlPos:     fptr(5000),
		TotalMarginUsed: fptr(800),
		AsOf:            time.Now().UTC().Truncate(time.Microsecond),
	}
}

// POS-I-01: ReplacePositions idempotent (run twice -> same rows).
func TestRepo_ReplacePositions_Idempotent(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	t.Cleanup(pool.Close)
	repo := NewRepository(pool)
	venueID := testVenue(t, pool)
	addr := testAddr()
	cleanupAddrs(t, pool, venueID, addr)
	if _, _, err := repo.UpsertRegistry(ctx, venueID, addr, SourceLeaderboard, nil, nil); err != nil {
		t.Fatalf("registry: %v", err)
	}

	snap := snapWith(
		Position{Coin: "BTC", Side: "LONG", Size: 0.5, EntryPrice: fptr(60000), MarkPrice: fptr(63000)},
		Position{Coin: "ETH", Side: "SHORT", Size: 2, EntryPrice: fptr(3000), MarkPrice: fptr(2900)},
	)
	if err := repo.ReplacePositions(ctx, venueID, addr, snap); err != nil {
		t.Fatalf("replace 1: %v", err)
	}
	if err := repo.ReplacePositions(ctx, venueID, addr, snap); err != nil {
		t.Fatalf("replace 2: %v", err)
	}
	got, err := repo.GetPositions(ctx, venueID, addr)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 positions, got %d: %+v", len(got), got)
	}
	// Deterministic order: coin ASC.
	if got[0].Coin != "BTC" || got[1].Coin != "ETH" {
		t.Errorf("order must be coin ASC: %+v", got)
	}
	if got[0].Side != "LONG" || got[0].Size != 0.5 {
		t.Errorf("btc mismatch: %+v", got[0])
	}
	sum, err := repo.GetPositionSummary(ctx, venueID, addr)
	if err != nil || sum == nil {
		t.Fatalf("summary: %v %+v", err, sum)
	}
	if sum.AccountValue == nil || *sum.AccountValue != 10000 {
		t.Errorf("summary account_value: %+v", sum)
	}
}

// POS-I-02: Coin removal (present in snap A, absent in snap B -> deleted).
func TestRepo_ReplacePositions_CoinRemoval(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	t.Cleanup(pool.Close)
	repo := NewRepository(pool)
	venueID := testVenue(t, pool)
	addr := testAddr()
	cleanupAddrs(t, pool, venueID, addr)
	if _, _, err := repo.UpsertRegistry(ctx, venueID, addr, SourceLeaderboard, nil, nil); err != nil {
		t.Fatalf("registry: %v", err)
	}

	snapA := snapWith(
		Position{Coin: "BTC", Side: "LONG", Size: 0.5, EntryPrice: fptr(60000)},
		Position{Coin: "ETH", Side: "SHORT", Size: 2, EntryPrice: fptr(3000)},
	)
	if err := repo.ReplacePositions(ctx, venueID, addr, snapA); err != nil {
		t.Fatalf("replace A: %v", err)
	}
	snapB := snapWith(
		Position{Coin: "BTC", Side: "LONG", Size: 1.0, EntryPrice: fptr(61000)},
	)
	snapB.AccountValue = fptr(11000)
	if err := repo.ReplacePositions(ctx, venueID, addr, snapB); err != nil {
		t.Fatalf("replace B: %v", err)
	}
	got, err := repo.GetPositions(ctx, venueID, addr)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got) != 1 || got[0].Coin != "BTC" {
		t.Fatalf("ETH must be deleted, BTC kept: %+v", got)
	}
	if got[0].Size != 1.0 {
		t.Errorf("BTC must be updated to size 1.0: %+v", got[0])
	}
}

// POS-I-03: FK cascade (DELETE registry -> positions + summary + trades gone).
func TestRepo_Positions_FKCascade(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	t.Cleanup(pool.Close)
	repo := NewRepository(pool)
	venueID := testVenue(t, pool)
	addr := testAddr()
	cleanupAddrs(t, pool, venueID, addr)
	if _, _, err := repo.UpsertRegistry(ctx, venueID, addr, SourceLeaderboard, nil, nil); err != nil {
		t.Fatalf("registry: %v", err)
	}
	if err := repo.ReplacePositions(ctx, venueID, addr, snapWith(
		Position{Coin: "BTC", Side: "LONG", Size: 0.5},
	)); err != nil {
		t.Fatalf("positions: %v", err)
	}
	day := time.Now().UTC().Truncate(24 * time.Hour)
	open := day.Add(1 * time.Hour)
	closeT := day.Add(2 * time.Hour)
	if err := repo.ReplaceTradesForDay(ctx, venueID, addr, day, []CompletedTrade{
		{Market: "BTC", Long: true, OpenTime: open, CloseTime: closeT, Volume: 100, PnL: 10, Fees: 1, Fills: 2},
	}); err != nil {
		t.Fatalf("trades: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`DELETE FROM trader_registry WHERE venue_id = $1 AND wallet_address = $2`,
		venueID, addr); err != nil {
		t.Fatalf("delete registry: %v", err)
	}
	if got, _ := repo.GetPositions(ctx, venueID, addr); len(got) != 0 {
		t.Errorf("positions must cascade-delete: %+v", got)
	}
	if sum, _ := repo.GetPositionSummary(ctx, venueID, addr); sum != nil {
		t.Errorf("summary must cascade-delete: %+v", sum)
	}
	rows, err := repo.ListTrades(ctx, venueID, addr, 10, nil, "", nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("trades must cascade-delete: %+v", rows)
	}
}

// POS-I-04: ReplaceTradesForDay deterministic (same day twice; then empty clears).
func TestRepo_ReplaceTradesForDay_Deterministic(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	t.Cleanup(pool.Close)
	repo := NewRepository(pool)
	venueID := testVenue(t, pool)
	addr := testAddr()
	cleanupAddrs(t, pool, venueID, addr)
	if _, _, err := repo.UpsertRegistry(ctx, venueID, addr, SourceLeaderboard, nil, nil); err != nil {
		t.Fatalf("registry: %v", err)
	}
	day := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	trades := []CompletedTrade{
		{Market: "BTC", Long: true, OpenTime: day.Add(1 * time.Hour), CloseTime: day.Add(2 * time.Hour), Volume: 30000, PnL: 1500, Fees: 30, Fills: 3},
		{Market: "ETH", Long: false, OpenTime: day.Add(3 * time.Hour), CloseTime: day.Add(4 * time.Hour), Volume: 5000, PnL: -100, Fees: 5, Fills: 2},
	}
	if err := repo.ReplaceTradesForDay(ctx, venueID, addr, day, trades); err != nil {
		t.Fatalf("replace 1: %v", err)
	}
	if err := repo.ReplaceTradesForDay(ctx, venueID, addr, day, trades); err != nil {
		t.Fatalf("replace 2: %v", err)
	}
	rows, err := repo.ListTrades(ctx, venueID, addr, 10, nil, "", nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("want 2 rows after idempotent recompute, got %d", len(rows))
	}
	// Empty recompute for the same day must delete stale day rows.
	if err := repo.ReplaceTradesForDay(ctx, venueID, addr, day, nil); err != nil {
		t.Fatalf("replace empty: %v", err)
	}
	rows, err = repo.ListTrades(ctx, venueID, addr, 10, nil, "", nil)
	if err != nil {
		t.Fatalf("list after empty: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("empty recompute must clear the day: %+v", rows)
	}
}
