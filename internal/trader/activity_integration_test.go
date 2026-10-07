//go:build integration

package trader

import (
	"context"
	"testing"
	"time"
)

// ACT-I-01: ListTrades keyset pagination (newest-first, no dup/skip).
func TestRepo_ListTrades_Keyset(t *testing.T) {
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
	day1 := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	// 3 closed trades: two on day2 (BTC newest, ETH older), one on day1.
	if err := repo.ReplaceTradesForDay(ctx, venueID, addr, day1, []CompletedTrade{
		{Market: "SOL", Long: true, OpenTime: day1.Add(1 * time.Hour), CloseTime: day1.Add(2 * time.Hour), Volume: 1000, PnL: 50, Fees: 2, Fills: 1},
	}); err != nil {
		t.Fatalf("day1: %v", err)
	}
	if err := repo.ReplaceTradesForDay(ctx, venueID, addr, day2, []CompletedTrade{
		{Market: "BTC", Long: true, OpenTime: day2.Add(5 * time.Hour), CloseTime: day2.Add(6 * time.Hour), Volume: 30000, PnL: 1500, Fees: 30, Fills: 3},
		{Market: "ETH", Long: false, OpenTime: day2.Add(1 * time.Hour), CloseTime: day2.Add(2 * time.Hour), Volume: 5000, PnL: -100, Fees: 5, Fills: 2},
	}); err != nil {
		t.Fatalf("day2: %v", err)
	}

	// Page 1: limit 2 -> newest 2 (BTC, ETH), fetch limit+1 to detect has_more.
	page1, err := repo.ListTrades(ctx, venueID, addr, 2+1, nil, "", nil)
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if len(page1) != 3 {
		t.Fatalf("limit+1 fetch must return 3 (2 + has_more probe), got %d", len(page1))
	}
	hasMore := len(page1) > 2
	if !hasMore {
		t.Fatal("page1 must have more")
	}
	page1 = page1[:2]
	if page1[0].Market != "BTC" || page1[1].Market != "ETH" {
		t.Fatalf("page1 order must be newest-first (BTC, ETH): %+v", page1)
	}
	if page1[0].ClosedAt.Before(page1[1].ClosedAt) {
		t.Errorf("closed_at DESC violated: %+v", page1)
	}

	// Page 2 via cursor triple from last row of page 1.
	last := page1[1]
	page2, err := repo.ListTrades(ctx, venueID, addr, 2+1, &last.ClosedAt, last.Market, &last.OpenedAt)
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(page2) != 1 || page2[0].Market != "SOL" {
		t.Fatalf("page2 must be the last row (SOL), got %+v", page2)
	}
	// No dup/skip across pages.
	seen := map[string]bool{}
	for _, r := range append(page1, page2...) {
		k := r.Market + r.ClosedAt.UTC().Format(time.RFC3339Nano) + r.OpenedAt.UTC().Format(time.RFC3339Nano)
		if seen[k] {
			t.Errorf("duplicate row across pages: %+v", r)
		}
		seen[k] = true
	}
	if len(seen) != 3 {
		t.Errorf("want 3 distinct rows, got %d", len(seen))
	}
}

// ACT-I-02: PruneTrades deletes only rows older than the 15d cutoff.
func TestRepo_PruneTrades_Cutoff(t *testing.T) {
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
	now := time.Now().UTC().Truncate(time.Microsecond)
	oldDay := now.Add(-20 * 24 * time.Hour).Truncate(24 * time.Hour)
	newDay := now.Add(-2 * 24 * time.Hour).Truncate(24 * time.Hour)
	if err := repo.ReplaceTradesForDay(ctx, venueID, addr, oldDay, []CompletedTrade{
		{Market: "OLD", Long: true, OpenTime: oldDay.Add(time.Hour), CloseTime: oldDay.Add(2 * time.Hour), Volume: 100, PnL: 10, Fees: 1, Fills: 1},
	}); err != nil {
		t.Fatalf("old: %v", err)
	}
	if err := repo.ReplaceTradesForDay(ctx, venueID, addr, newDay, []CompletedTrade{
		{Market: "NEW", Long: true, OpenTime: newDay.Add(time.Hour), CloseTime: newDay.Add(2 * time.Hour), Volume: 200, PnL: 20, Fees: 2, Fills: 1},
	}); err != nil {
		t.Fatalf("new: %v", err)
	}
	cutoff := now.Add(-15 * 24 * time.Hour)
	deleted, err := repo.PruneTrades(ctx, venueID, addr, cutoff)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if deleted != 1 {
		t.Errorf("must delete exactly 1 old row, deleted %d", deleted)
	}
	rows, err := repo.ListTrades(ctx, venueID, addr, 10, nil, "", nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 || rows[0].Market != "NEW" {
		t.Errorf("only NEW must remain: %+v", rows)
	}
}
