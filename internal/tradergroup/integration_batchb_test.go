//go:build integration

package tradergroup

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/trader"
)

func ptrFloat(v float64) *float64 { return &v }

// GRP-STATS: null/partial rules for ROI and max drawdown.
func TestRepo_GroupStats_NullPartial(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	t.Cleanup(pool.Close)
	repo := NewRepository(pool)
	_, userA := testUser(t, pool)
	venueID := testVenueID(t, pool)
	addrs := seedRegistry(t, pool, venueID, 2)

	g, err := repo.Create(ctx, userA, "StatsGroup", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM trader_groups WHERE id = $1`, g.ID) })
	if _, _, err := repo.AddMembers(ctx, g.ID, userA, []MemberInput{
		{Venue: "tradergroup-test-venue", WalletAddress: addrs[0]},
		{Venue: "tradergroup-test-venue", WalletAddress: addrs[1]},
	}); err != nil {
		t.Fatalf("add: %v", err)
	}

	tr := trader.NewRepository(pool)
	day := time.Now().UTC().AddDate(0, 0, -2).Truncate(24 * time.Hour)
	// equity history for only one wallet
	if err := tr.UpsertEquityDaily(ctx, venueID, addrs[0], trader.EquityDay{
		Date: day, StartEquity: ptrFloat(100), EndEquity: ptrFloat(110), PeakEquity: ptrFloat(110),
	}); err != nil {
		t.Fatalf("equity: %v", err)
	}
	if err := tr.UpsertDailyStats(ctx, &trader.DailyStats{
		VenueID: venueID, WalletAddress: addrs[0], StatDate: day,
		RealizedPnL: ptrFloat(5), Volume: ptrFloat(10), TradeCount: 1,
	}); err != nil {
		t.Fatalf("daily: %v", err)
	}
	_, _ = tr.EnsureSyncState(ctx, venueID, addrs[0])
	_, _ = pool.Exec(ctx, `UPDATE trader_sync_state SET sync_status='ready', last_positions_sync_at=NOW() WHERE venue_id=$1 AND wallet_address=$2`, venueID, addrs[0])

	st, err := repo.Stats(ctx, g.ID, userA, "30D")
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if st.ROI != nil {
		t.Errorf("roi must be null when a member lacks equity history, got %v", *st.ROI)
	}
	if st.MaxDrawdownPct != nil {
		t.Errorf("max_drawdown_pct must be null, got %v", *st.MaxDrawdownPct)
	}
	if !st.Partial {
		t.Errorf("partial must be true")
	}
	if st.MissingWallets < 1 {
		t.Errorf("missing_wallets must include equity-lacking wallet, got %d", st.MissingWallets)
	}

	// seed equity for second wallet -> ROI computable
	if err := tr.UpsertEquityDaily(ctx, venueID, addrs[1], trader.EquityDay{
		Date: day, StartEquity: ptrFloat(200), EndEquity: ptrFloat(210), PeakEquity: ptrFloat(210),
	}); err != nil {
		t.Fatalf("equity2: %v", err)
	}
	_, _ = tr.EnsureSyncState(ctx, venueID, addrs[1])
	_, _ = pool.Exec(ctx, `UPDATE trader_sync_state SET sync_status='ready', last_positions_sync_at=NOW() WHERE venue_id=$1 AND wallet_address=$2`, venueID, addrs[1])
	st2, err := repo.Stats(ctx, g.ID, userA, "30D")
	if err != nil {
		t.Fatalf("stats2: %v", err)
	}
	if st2.ROI == nil {
		t.Errorf("roi must be non-null when all members have equity history")
	}
	if st2.MaxDrawdownPct == nil {
		t.Errorf("max_drawdown_pct must be non-null when all members have equity history")
	}
}

// GRP-POSITIONS: partial/missing based on positions & unrealized data.
func TestRepo_GroupPositions_Partial(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	t.Cleanup(pool.Close)
	repo := NewRepository(pool)
	_, userA := testUser(t, pool)
	venueID := testVenueID(t, pool)
	addrs := seedRegistry(t, pool, venueID, 2)

	g, err := repo.Create(ctx, userA, "PosGroup", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM trader_groups WHERE id = $1`, g.ID) })
	if _, _, err := repo.AddMembers(ctx, g.ID, userA, []MemberInput{
		{Venue: "tradergroup-test-venue", WalletAddress: addrs[0]},
		{Venue: "tradergroup-test-venue", WalletAddress: addrs[1]},
	}); err != nil {
		t.Fatalf("add: %v", err)
	}

	tr := trader.NewRepository(pool)
	pv := 100.0
	upnl := 5.0
	if err := tr.ReplacePositions(ctx, venueID, addrs[0], &trader.PositionSnapshot{
		AccountValue: ptrFloat(1000),
		Positions:    []trader.Position{{Coin: "BTC", Side: "LONG", Size: 1, PositionValue: &pv, UnrealizedPnl: &upnl}},
		AsOf:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("positions: %v", err)
	}

	resp, err := repo.Positions(ctx, g.ID, userA)
	if err != nil {
		t.Fatalf("positions: %v", err)
	}
	if !resp.Partial {
		t.Errorf("partial must be true when a wallet lacks positions")
	}
	if resp.MissingWallets < 1 {
		t.Errorf("missing_wallets must include wallet without positions, got %d", resp.MissingWallets)
	}
	if len(resp.Positions) == 0 {
		t.Errorf("expected aggregated positions")
	}

	// wallet0 loses unrealized data -> missing
	pv2 := 100.0
	if err := tr.ReplacePositions(ctx, venueID, addrs[0], &trader.PositionSnapshot{
		AccountValue: ptrFloat(1000),
		Positions:    []trader.Position{{Coin: "BTC", Side: "LONG", Size: 1, PositionValue: &pv2}},
		AsOf:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("positions2: %v", err)
	}
	resp2, err := repo.Positions(ctx, g.ID, userA)
	if err != nil {
		t.Fatalf("positions2: %v", err)
	}
	if !resp2.Partial {
		t.Errorf("partial must be true when unrealized data missing")
	}
	if resp2.MissingWallets < 1 {
		t.Errorf("missing_wallets must include wallet without unrealized data, got %d", resp2.MissingWallets)
	}
}

// GRP-PERF: 1D cumulative realized pnl null; metric availability.
func TestRepo_GroupPerformance_NullPartial(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	t.Cleanup(pool.Close)
	repo := NewRepository(pool)
	_, userA := testUser(t, pool)
	venueID := testVenueID(t, pool)
	addrs := seedRegistry(t, pool, venueID, 1)

	g, err := repo.Create(ctx, userA, "PerfGroup", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM trader_groups WHERE id = $1`, g.ID) })
	if _, _, err := repo.AddMembers(ctx, g.ID, userA, []MemberInput{
		{Venue: "tradergroup-test-venue", WalletAddress: addrs[0]},
	}); err != nil {
		t.Fatalf("add: %v", err)
	}

	tr := trader.NewRepository(pool)
	day1 := time.Now().UTC().AddDate(0, 0, -2).Truncate(24 * time.Hour)
	day2 := time.Now().UTC().AddDate(0, 0, -1).Truncate(24 * time.Hour)
	for _, d := range []time.Time{day1, day2} {
		if err := tr.UpsertEquityDaily(ctx, venueID, addrs[0], trader.EquityDay{
			Date: d, StartEquity: ptrFloat(100), EndEquity: ptrFloat(110), PeakEquity: ptrFloat(110),
		}); err != nil {
			t.Fatalf("equity: %v", err)
		}
	}
	// daily stats on day1 only
	if err := tr.UpsertDailyStats(ctx, &trader.DailyStats{
		VenueID: venueID, WalletAddress: addrs[0], StatDate: day1,
		RealizedPnL: ptrFloat(5), Volume: ptrFloat(10), TradeCount: 1,
	}); err != nil {
		t.Fatalf("daily: %v", err)
	}

	// 30D daily
	perf, err := repo.Performance(ctx, g.ID, userA, "30D")
	if err != nil {
		t.Fatalf("perf: %v", err)
	}
	if len(perf.Points) < 2 {
		t.Fatalf("expected >=2 points, got %d", len(perf.Points))
	}
	hasCum := false
	for _, p := range perf.Points {
		if p.CumulativeRealizedPnL != nil {
			hasCum = true
		}
	}
	if !hasCum {
		t.Errorf("expected cumulative_realized_pnl non-nil when daily rows exist")
	}
	if perf.InsufficientData {
		t.Errorf("insufficient_data must be false with >=2 points")
	}
	if !perf.MetricAvailability.CumulativeRealizedPnL {
		t.Errorf("metric_availability cumulative_realized_pnl must be true")
	}

	// 1D intraday
	perf1, err := repo.Performance(ctx, g.ID, userA, "1D")
	if err != nil {
		t.Fatalf("perf1: %v", err)
	}
	if perf1.Granularity != "intraday" {
		t.Errorf("granularity must be intraday for 1D")
	}
	if perf1.MetricAvailability.CumulativeRealizedPnL {
		t.Errorf("cumulative_realized_pnl must be false for 1D")
	}
	for _, p := range perf1.Points {
		if p.CumulativeRealizedPnL != nil {
			t.Errorf("cumulative_realized_pnl point must be nil for 1D")
		}
	}
}

// GRP-COMPARE: overlap counts distinct shared wallets.
func TestRepo_GroupCompare_Overlap(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	t.Cleanup(pool.Close)
	repo := NewRepository(pool)
	_, userA := testUser(t, pool)
	venueID := testVenueID(t, pool)
	addrs := seedRegistry(t, pool, venueID, 3)

	g1, err := repo.Create(ctx, userA, "G1", "")
	if err != nil {
		t.Fatalf("create g1: %v", err)
	}
	g2, err := repo.Create(ctx, userA, "G2", "")
	if err != nil {
		t.Fatalf("create g2: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM trader_groups WHERE id IN ($1,$2)`, g1.ID, g2.ID)
	})
	if _, _, err := repo.AddMembers(ctx, g1.ID, userA, []MemberInput{
		{Venue: "tradergroup-test-venue", WalletAddress: addrs[0]},
		{Venue: "tradergroup-test-venue", WalletAddress: addrs[1]},
	}); err != nil {
		t.Fatalf("add g1: %v", err)
	}
	if _, _, err := repo.AddMembers(ctx, g2.ID, userA, []MemberInput{
		{Venue: "tradergroup-test-venue", WalletAddress: addrs[0]},
		{Venue: "tradergroup-test-venue", WalletAddress: addrs[2]},
	}); err != nil {
		t.Fatalf("add g2: %v", err)
	}

	resp, err := repo.Compare(ctx, userA, []uuid.UUID{g1.ID, g2.ID}, "30D")
	if err != nil {
		t.Fatalf("compare: %v", err)
	}
	if len(resp.Groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(resp.Groups))
	}
	if len(resp.Overlap.Pairwise) != 1 {
		t.Fatalf("expected 1 pairwise overlap, got %d", len(resp.Overlap.Pairwise))
	}
	if resp.Overlap.Pairwise[0].SharedWallets != 1 {
		t.Errorf("shared_wallets must be 1 (distinct), got %d", resp.Overlap.Pairwise[0].SharedWallets)
	}
}
