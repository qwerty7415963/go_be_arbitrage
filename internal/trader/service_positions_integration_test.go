//go:build integration

package trader

import (
	"context"
	"testing"
	"time"
)

func serviceFixture(t *testing.T) (*Repository, *Service, string) {
	t.Helper()
	pool := testPool(t)
	t.Cleanup(pool.Close)
	repo := NewRepository(pool)
	venueID := testVenue(t, pool)
	addr := testAddr()
	cleanupAddrs(t, pool, venueID, addr)
	ctx := context.Background()
	if _, _, err := repo.UpsertRegistry(ctx, venueID, addr, SourceLeaderboard, nil, nil); err != nil {
		t.Fatalf("registry: %v", err)
	}
	svc := NewService(repo, nil, []byte("test-secret"))
	return repo, svc, addr
}

// Positions: seeded snapshot -> ready + rows; unknown wallet 404.
func TestService_Positions_Ready(t *testing.T) {
	ctx := context.Background()
	repo, svc, addr := serviceFixture(t)
	venueID, _ := repo.VenueIDByCode(ctx, testVenueCode)

	if err := repo.ReplacePositions(ctx, venueID, addr, posSnap("BTC")); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := repo.EnsureSyncState(ctx, venueID, addr); err != nil {
		t.Fatalf("sync state ensure: %v", err)
	}
	now := time.Now().UTC()
	if _, err := repo.pool.Exec(ctx, `UPDATE trader_sync_state SET last_positions_sync_at=$3 WHERE venue_id=$1 AND wallet_address=$2`, venueID, addr, now); err != nil {
		t.Fatalf("sync state: %v", err)
	}
	dto, err := svc.Positions(ctx, testVenueCode, addr)
	if err != nil {
		t.Fatalf("positions: %v", err)
	}
	if dto.DataStatus != DataReady {
		t.Errorf("status: %v", dto.DataStatus)
	}
	if dto.Summary == nil || len(dto.Positions) != 1 || dto.Positions[0].Coin != "BTC" {
		t.Errorf("dto: %+v", dto)
	}
	if dto.AsOf == nil {
		t.Error("as_of must be set")
	}
	if _, err := svc.Positions(ctx, testVenueCode, testAddr()); err == nil {
		t.Error("unknown wallet must 404")
	}
}

// Positions M5: never synced -> syncing, summary null, positions [].
func TestService_Positions_NeverSynced(t *testing.T) {
	ctx := context.Background()
	_, svc, addr := serviceFixture(t)
	dto, err := svc.Positions(ctx, testVenueCode, addr)
	if err != nil {
		t.Fatalf("positions: %v", err)
	}
	if dto.DataStatus != DataSyncing {
		t.Errorf("M5: never synced must be syncing, got %v", dto.DataStatus)
	}
	if dto.Summary != nil {
		t.Errorf("summary must be null: %+v", dto.Summary)
	}
	if dto.Positions == nil || len(dto.Positions) != 0 {
		t.Errorf("positions must be []: %+v", dto.Positions)
	}
}

// Activity: 3 trades, limit 2 -> page walk with net_pnl; bad cursor 400.
func TestService_Activity_PageWalk(t *testing.T) {
	ctx := context.Background()
	repo, svc, addr := serviceFixture(t)
	venueID, _ := repo.VenueIDByCode(ctx, testVenueCode)

	day1 := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	if err := repo.ReplaceTradesForDay(ctx, venueID, addr, day1, []CompletedTrade{
		{Market: "SOL", Long: true, OpenTime: day1.Add(time.Hour), CloseTime: day1.Add(2 * time.Hour), Volume: 1000, PnL: 50, Fees: 2, Fills: 1},
	}); err != nil {
		t.Fatalf("day1: %v", err)
	}
	if err := repo.ReplaceTradesForDay(ctx, venueID, addr, day2, []CompletedTrade{
		{Market: "BTC", Long: true, OpenTime: day2.Add(5 * time.Hour), CloseTime: day2.Add(6 * time.Hour), Volume: 30000, PnL: 1500, Fees: 30, Fills: 3},
		{Market: "ETH", Long: false, OpenTime: day2.Add(time.Hour), CloseTime: day2.Add(2 * time.Hour), Volume: 5000, PnL: -100, Fees: 5, Fills: 2},
	}); err != nil {
		t.Fatalf("day2: %v", err)
	}

	p1, err := svc.Activity(ctx, testVenueCode, addr, 2, "")
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if len(p1.Rows) != 2 || !p1.HasMore || p1.NextCursor == "" {
		t.Fatalf("page1: %+v", p1)
	}
	if p1.Rows[0].Market != "BTC" || p1.Rows[0].NetPnl != 1470 {
		t.Errorf("page1 rows: %+v", p1.Rows)
	}
	p2, err := svc.Activity(ctx, testVenueCode, addr, 2, p1.NextCursor)
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(p2.Rows) != 1 || p2.Rows[0].Market != "SOL" || p2.HasMore {
		t.Errorf("page2: %+v", p2)
	}
	if _, err := svc.Activity(ctx, testVenueCode, addr, 2, "forged.cursor"); err == nil {
		t.Error("bad cursor must 400")
	}
	if _, err := svc.Activity(ctx, testVenueCode, addr, 101, ""); err == nil {
		t.Error("limit 101 must 400")
	}
}
