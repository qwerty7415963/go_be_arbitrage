//go:build integration

package trader

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

var addrSeq atomic.Uint64

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	host := os.Getenv("ARBITRAGE_DB_HOST")
	if host == "" {
		host = "localhost"
	}
	port := os.Getenv("ARBITRAGE_DB_PORT")
	if port == "" {
		port = "5433"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx,
		fmt.Sprintf("postgres://test:test@%s:%s/arbitrage_test?sslmode=disable", host, port))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("ping (is docker running?): %v", err)
	}
	return pool
}

// testVenueCode is a dedicated venue so suites stay hermetic: the live
// server's workers only ever write venue=hyperliquid rows.
const testVenueCode = "trader-test-venue"

func testVenue(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
		INSERT INTO venues (code, name, venue_type)
		VALUES ('trader-test-venue', 'Trader Test Venue', 'PERP_DEX')
		ON CONFLICT (code) DO NOTHING`); err != nil {
		t.Fatalf("venue: %v", err)
	}
	var id uuid.UUID
	if err := pool.QueryRow(ctx,
		`SELECT id FROM venues WHERE code = 'trader-test-venue'`).Scan(&id); err != nil {
		t.Fatalf("venue id: %v", err)
	}
	return id
}

func testAddr() string {
	n := addrSeq.Add(1)
	return fmt.Sprintf("0x%040x", uint64(time.Now().UnixNano())%0xffff+n*0x10000)
}

func cleanupAddrs(t *testing.T, pool *pgxpool.Pool, venueID uuid.UUID, addrs ...string) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		for _, a := range addrs {
			_, _ = pool.Exec(ctx,
				`DELETE FROM trader_registry WHERE venue_id = $1 AND wallet_address = $2`,
				venueID, a)
		}
	})
}

// DISC-I-01: leaderboard sync twice — one row, display updated,
// first_seen_at untouched (BE-001/BE-002); source merge → both (BE-005).
func TestRepo_UpsertRegistry_Idempotent(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	t.Cleanup(pool.Close)
	repo := NewRepository(pool)
	venueID := testVenue(t, pool)
	addr := testAddr()
	cleanupAddrs(t, pool, venueID, addr)

	name1, name2 := "Alpha", "Alpha Prime"
	first, created, err := repo.UpsertRegistry(ctx, venueID, addr, SourceLeaderboard, &name1, nil)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if !created {
		t.Error("first upsert must report created=true")
	}
	if first.DiscoverySource != SourceLeaderboard || first.LeaderboardSeenAt == nil {
		t.Errorf("leaderboard row: %+v", first)
	}

	second, created, err := repo.UpsertRegistry(ctx, venueID, addr, SourceLeaderboard, &name2, nil)
	if err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	if created {
		t.Error("second upsert must report created=false")
	}
	if !second.FirstSeenAt.Equal(first.FirstSeenAt) {
		t.Errorf("first_seen_at moved: %v -> %v", first.FirstSeenAt, second.FirstSeenAt)
	}
	if second.DisplayName == nil || *second.DisplayName != name2 {
		t.Errorf("display_name not updated: %+v", second.DisplayName)
	}

	third, _, err := repo.UpsertRegistry(ctx, venueID, addr, SourceWSTrade, nil, nil)
	if err != nil {
		t.Fatalf("ws upsert: %v", err)
	}
	if third.DiscoverySource != SourceBoth {
		t.Errorf("source merge: want both, got %s", third.DiscoverySource)
	}
	if third.DisplayName == nil || *third.DisplayName != name2 {
		t.Error("null display_name must not clear the existing one")
	}

	fourth, _, err := repo.UpsertRegistry(ctx, venueID, addr, SourceManual, nil, nil)
	if err != nil {
		t.Fatalf("manual upsert: %v", err)
	}
	if fourth.DiscoverySource != SourceBoth {
		t.Errorf("manual must not overwrite source: %s", fourth.DiscoverySource)
	}

	if _, err := repo.GetRegistry(ctx, venueID, addr); err != nil {
		t.Errorf("get: %v", err)
	}
	if _, err := repo.GetRegistry(ctx, venueID, testAddr()); err == nil {
		t.Error("unknown wallet must return an error")
	}
}

// Sync state: ensure → pending; progress advances cursors/status/retries.
func TestRepo_SyncState_EnsureAndProgress(t *testing.T) {
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

	s, err := repo.EnsureSyncState(ctx, venueID, addr)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if s.SyncStatus != "pending" || s.RetryCount != 0 {
		t.Errorf("fresh state: %+v", s)
	}

	ts := time.Now().UTC().Truncate(time.Microsecond)
	tid := int64(987654321)
	msg := "boom"
	if err := repo.UpdateSyncProgress(ctx, venueID, addr, &ts, &tid, "error", &msg, 2); err != nil {
		t.Fatalf("progress: %v", err)
	}
	s, _ = repo.GetSyncState(ctx, venueID, addr)
	if s.SyncStatus != "error" || s.RetryCount != 2 || s.LastError == nil || *s.LastError != msg {
		t.Errorf("progress not applied: %+v", s)
	}
	if s.FillsLastTID == nil || *s.FillsLastTID != tid {
		t.Errorf("cursor not applied: %+v", s)
	}

	empty := ""
	if err := repo.UpdateSyncProgress(ctx, venueID, addr, nil, nil, "ready", &empty, -5); err != nil {
		t.Fatalf("clear: %v", err)
	}
	s, _ = repo.GetSyncState(ctx, venueID, addr)
	if s.SyncStatus != "ready" || s.LastError != nil || s.RetryCount != 0 {
		t.Errorf("clear not applied: %+v", s)
	}
}

// MET-U-05 (repo half): same day recomputed → identical row.
func TestRepo_DailyStats_Idempotent(t *testing.T) {
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

	pnl, vol, gp, gl := 1234.5, 98765.0, 2000.0, -765.5
	day := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	row := &DailyStats{VenueID: venueID, WalletAddress: addr, StatDate: day,
		TradeCount: 10, WinCount: 6, LossCount: 3, BreakevenCount: 1,
		RealizedPnL: &pnl, Fees: 12.5, Volume: &vol, GrossProfit: &gp, GrossLoss: &gl,
		LongCount: 7, LongWins: 5, ShortCount: 3, ShortWins: 1,
		HoldingTimeSecSum: 3600.5, HoldingTimeSecCount: 10}
	if err := repo.UpsertDailyStats(ctx, row); err != nil {
		t.Fatalf("upsert 1: %v", err)
	}
	if err := repo.UpsertDailyStats(ctx, row); err != nil {
		t.Fatalf("upsert 2: %v", err)
	}
	got, err := repo.GetDailyStats(ctx, venueID, addr, day)
	if err != nil || got == nil {
		t.Fatalf("get: %v", err)
	}
	if got.TradeCount != 10 || got.WinCount != 6 || *got.RealizedPnL != pnl ||
		*got.GrossLoss != gl || got.HoldingTimeSecCount != 10 {
		t.Errorf("row mismatch: %+v", got)
	}
	if missing, _ := repo.GetDailyStats(ctx, venueID, addr, day.AddDate(0, 0, -1)); missing != nil {
		t.Error("absent day must return nil")
	}
}

// Period metrics roundtrip incl. null profit_factor + status/version.
func TestRepo_PeriodMetrics_Roundtrip(t *testing.T) {
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

	pnl, roi, wr := 15000.0, 12.5, 66.6667
	var tc int64 = 9
	m := &PeriodMetrics{VenueID: venueID, WalletAddress: addr, Period: Period30D,
		AsOf: time.Now().UTC(), PnL: &pnl, ROI: &roi, WinRate: &wr, TradeCount: &tc,
		DataStatus: DataReady, CalculationVersion: CurrentCalculationVersion}
	if err := repo.UpsertPeriodMetrics(ctx, m); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, err := repo.GetPeriodMetrics(ctx, venueID, addr, Period30D)
	if err != nil || got == nil {
		t.Fatalf("get: %v", err)
	}
	if *got.PnL != pnl || *got.ROI != roi || *got.WinRate != wr || *got.TradeCount != tc {
		t.Errorf("metrics mismatch: %+v", got)
	}
	if got.ProfitFactor != nil {
		t.Errorf("null profit_factor must roundtrip as null: %+v", got.ProfitFactor)
	}
	if got.Venue != testVenueCode || got.DataStatus != DataReady ||
		got.CalculationVersion != CurrentCalculationVersion {
		t.Errorf("meta mismatch: %+v", got)
	}
}

// Leaderboard ref roundtrip (ROI passthrough source, spec D4).
func TestRepo_LeaderboardRef_Roundtrip(t *testing.T) {
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

	roi := 0.0123 // fraction, as upstream sends it
	ref := &LeaderboardRef{VenueID: venueID, WalletAddress: addr, Window: "month",
		ROI: &roi, FetchedAt: time.Now().UTC()}
	if err := repo.UpsertLeaderboardRef(ctx, ref); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, err := repo.GetLeaderboardRef(ctx, venueID, addr, "month")
	if err != nil || got == nil {
		t.Fatalf("get: %v", err)
	}
	if got.ROI == nil || *got.ROI != roi {
		t.Errorf("ref mismatch: %+v", got)
	}
	if missing, _ := repo.GetLeaderboardRef(ctx, venueID, addr, "day"); missing != nil {
		t.Error("absent window must return nil")
	}
}
