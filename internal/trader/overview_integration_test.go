//go:build integration

package trader

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestRepo_Overview_Aggregates(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	t.Cleanup(pool.Close)
	repo := NewRepository(pool)
	venueID := testVenue(t, pool)
	addr1 := testAddr()
	addr2 := testAddr()
	cleanupAddrs(t, pool, venueID, addr1, addr2)

	// registry
	for _, a := range []string{addr1, addr2} {
		if _, _, err := repo.UpsertRegistry(ctx, venueID, a, SourceLeaderboard, nil, nil); err != nil {
			t.Fatalf("registry: %v", err)
		}
	}
	pnl1 := 1500000.0
	pnl2 := -50000.0
	vol1 := 100000.0
	vol2 := 20000.0
	tc1, tc2 := int64(10), int64(5)
	lc1, sc1 := int64(6), int64(4)
	lc2, sc2 := int64(2), int64(3)
	for i, a := range []string{addr1, addr2} {
		pnl := pnl1
		vol := vol1
		tc := tc1
		lc, sc := lc1, sc1
		if i == 1 {
			pnl, vol, tc, lc, sc = pnl2, vol2, tc2, lc2, sc2
		}
		m := &PeriodMetrics{VenueID: venueID, WalletAddress: a, Period: Period30D,
			AsOf: time.Now().UTC(), PnL: &pnl, RealizedPnL: &pnl, Volume: &vol,
			TradeCount: &tc, LongCount: &lc, ShortCount: &sc,
			DataStatus: DataReady, CalculationVersion: CurrentCalculationVersion}
		if err := repo.UpsertPeriodMetrics(ctx, m); err != nil {
			t.Fatalf("period: %v", err)
		}
	}

	// equity daily (latest dedup): addr1 two rows
	end1 := 5000000.0
	day1 := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -1)
	day0 := time.Now().UTC().Truncate(24 * time.Hour)
	if err := repo.UpsertEquityDaily(ctx, venueID, addr1, EquityDay{Date: day1, StartEquity: &end1, EndEquity: &end1, PeakEquity: &end1}); err != nil {
		t.Fatalf("eq: %v", err)
	}
	end1b := 6000000.0
	if err := repo.UpsertEquityDaily(ctx, venueID, addr1, EquityDay{Date: day0, StartEquity: &end1b, EndEquity: &end1b, PeakEquity: &end1b}); err != nil {
		t.Fatalf("eq2: %v", err)
	}
	end2 := 50000.0
	if err := repo.UpsertEquityDaily(ctx, venueID, addr2, EquityDay{Date: day0, StartEquity: &end2, EndEquity: &end2, PeakEquity: &end2}); err != nil {
		t.Fatalf("eq3: %v", err)
	}

	// daily stats for series
	rpnl := 100.0
	ds := &DailyStats{VenueID: venueID, WalletAddress: addr1, StatDate: day0,
		RealizedPnL: &rpnl, TradeCount: 1, LongCount: 1, ShortCount: 0}
	if err := repo.UpsertDailyStats(ctx, ds); err != nil {
		t.Fatalf("daily: %v", err)
	}

	// aggregates
	aggs, err := repo.AggregatePnLBuckets(ctx, venueID, Period30D, "realized_pnl")
	if err != nil {
		t.Fatalf("agg: %v", err)
	}
	if len(aggs) == 0 {
		t.Fatal("no agg")
	}
	// find extremely_profitable count 1
	found := false
	for _, a := range aggs {
		if a.Bucket == "extremely_profitable" {
			found = true
			if a.Count != 1 {
				t.Errorf("extremely count: %d", a.Count)
			}
		}
	}
	if !found {
		t.Error("extremely_profitable missing")
	}

	acct, err := repo.AggregateAccountBuckets(ctx, venueID, Period30D)
	if err != nil {
		t.Fatalf("acct: %v", err)
	}
	// addr1 latest equity 6,000,000 -> apex
	apexFound := false
	for _, a := range acct {
		if a.Bucket == "apex" {
			apexFound = true
			if a.Count != 1 {
				t.Errorf("apex count: %d", a.Count)
			}
			if a.EquitySum != 6000000.0 {
				t.Errorf("apex equity: %v", a.EquitySum)
			}
			if a.VolumeSum != vol1 {
				t.Errorf("apex volume: %v want %v", a.VolumeSum, vol1)
			}
		}
	}
	if !apexFound {
		t.Error("apex missing")
	}

	// account wallet count
	n, err := repo.AccountWalletCount(ctx, venueID)
	if err != nil || n != 2 {
		t.Errorf("account wallets: %d %v", n, err)
	}

	// pnl wallet count
	pn, err := repo.PnlWalletCount(ctx, venueID, Period30D, "realized_pnl")
	if err != nil || pn != 2 {
		t.Errorf("pnl wallets: %d %v", pn, err)
	}

	// universe size
	us, err := repo.UniverseSize(ctx, venueID, Period30D)
	if err != nil || us != 2 {
		t.Errorf("universe: %d %v", us, err)
	}

	// data status
	ds2, err := repo.DataStatusCounts(ctx, venueID, Period30D)
	if err != nil {
		t.Fatalf("ds: %v", err)
	}
	if ds2["ready"] != 2 {
		t.Errorf("ready: %d", ds2["ready"])
	}

	// series window
	since := time.Now().UTC().AddDate(0, 0, -29).Truncate(24 * time.Hour)
	rows, err := repo.AggregateDailyPnLSeries(ctx, venueID, Period30D, "realized_pnl", since)
	if err != nil {
		t.Fatalf("series: %v", err)
	}
	if len(rows) == 0 {
		t.Error("series empty")
	}

	// series wallet count
	sw, err := repo.SeriesWalletCount(ctx, venueID, Period30D, "realized_pnl", since)
	if err != nil || sw != 1 {
		t.Errorf("series wallets: %d %v", sw, err)
	}

	// service-level: account bucket sentiment derived from counts
	svc := NewService(repo, nil, nil)
	ov, err := svc.Overview(ctx, "trader-test-venue", Period30D, "realized_pnl", 30)
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	for _, b := range ov.AccountBuckets {
		if b.Sentiment != Sentiment(b.LongCount, b.ShortCount) {
			t.Errorf("account bucket %s sentiment: got %s want %s", b.ID, b.Sentiment, Sentiment(b.LongCount, b.ShortCount))
		}
	}

	// null equity excluded: insert a null equity row? Already covered by count filter.
	// venue/period isolation: different period has 0
	aggs2, err := repo.AggregatePnLBuckets(ctx, venueID, Period7D, "realized_pnl")
	if err != nil {
		t.Fatalf("agg7: %v", err)
	}
	if len(aggs2) != 0 {
		t.Errorf("7D should be empty: %d", len(aggs2))
	}
}
func TestRepo_Overview_NullEquityAndVenueIsolation(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	t.Cleanup(pool.Close)
	repo := NewRepository(pool)

	venueCode1 := fmt.Sprintf("trader-test-venue-%d", time.Now().UnixNano())
	if _, err := pool.Exec(ctx, `INSERT INTO venues (code, name, venue_type) VALUES ($1, 'Trader Test Venue', 'PERP_DEX')`, venueCode1); err != nil {
		t.Fatalf("venue1: %v", err)
	}
	var venueID1 uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM venues WHERE code = $1`, venueCode1).Scan(&venueID1); err != nil {
		t.Fatalf("venue1 id: %v", err)
	}

	addrNull := testAddr()
	cleanupAddrs(t, pool, venueID1, addrNull)
	if _, _, err := repo.UpsertRegistry(ctx, venueID1, addrNull, SourceLeaderboard, nil, nil); err != nil {
		t.Fatalf("registry: %v", err)
	}
	pnl := 1000.0
	vol := 100.0
	tc := int64(2)
	lc, sc := int64(1), int64(1)
	m := &PeriodMetrics{VenueID: venueID1, WalletAddress: addrNull, Period: Period30D,
		AsOf: time.Now().UTC(), PnL: &pnl, RealizedPnL: &pnl, Volume: &vol,
		TradeCount: &tc, LongCount: &lc, ShortCount: &sc,
		DataStatus: DataReady, CalculationVersion: CurrentCalculationVersion}
	if err := repo.UpsertPeriodMetrics(ctx, m); err != nil {
		t.Fatalf("period: %v", err)
	}
	nullEq := (*float64)(nil)
	if err := repo.UpsertEquityDaily(ctx, venueID1, addrNull, EquityDay{Date: time.Now().UTC().Truncate(24 * time.Hour), StartEquity: nullEq, EndEquity: nullEq, PeakEquity: nullEq}); err != nil {
		t.Fatalf("eq: %v", err)
	}

	n, err := repo.AccountWalletCount(ctx, venueID1)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("account wallets with null equity: got %d want 0", n)
	}

	// null equity excluded from account buckets
	acct, err := repo.AggregateAccountBuckets(ctx, venueID1, Period30D)
	if err != nil {
		t.Fatalf("acct: %v", err)
	}
	if len(acct) != 0 {
		t.Errorf("account buckets with null equity: got %d want 0", len(acct))
	}

	venueCode2 := fmt.Sprintf("trader-test-venue-%d", time.Now().UnixNano()+1)
	if _, err := pool.Exec(ctx, `INSERT INTO venues (code, name, venue_type) VALUES ($1, 'Trader Test Venue 2', 'PERP_DEX')`, venueCode2); err != nil {
		t.Fatalf("venue2: %v", err)
	}
	var venueID2 uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM venues WHERE code = $1`, venueCode2).Scan(&venueID2); err != nil {
		t.Fatalf("venue2 id: %v", err)
	}
	addr2 := testAddr()
	cleanupAddrs(t, pool, venueID2, addr2)
	if _, _, err := repo.UpsertRegistry(ctx, venueID2, addr2, SourceLeaderboard, nil, nil); err != nil {
		t.Fatalf("registry2: %v", err)
	}
	pnl2 := 50.0
	m2 := &PeriodMetrics{VenueID: venueID2, WalletAddress: addr2, Period: Period30D,
		AsOf: time.Now().UTC(), PnL: &pnl2, RealizedPnL: &pnl2, DataStatus: DataReady, CalculationVersion: CurrentCalculationVersion}
	if err := repo.UpsertPeriodMetrics(ctx, m2); err != nil {
		t.Fatalf("period2: %v", err)
	}

	aggs, err := repo.AggregatePnLBuckets(ctx, venueID1, Period30D, "realized_pnl")
	if err != nil {
		t.Fatalf("agg: %v", err)
	}
	found := false
	for _, a := range aggs {
		if a.Bucket == "profitable" {
			found = true
		}
	}
	if !found {
		t.Error("primary venue profitable bucket missing")
	}
	aggs2, err := repo.AggregatePnLBuckets(ctx, venueID2, Period30D, "realized_pnl")
	if err != nil {
		t.Fatalf("agg2: %v", err)
	}
	if len(aggs2) == 0 {
		t.Error("second venue aggregates empty")
	}
	// venue1 should not include venue2's wallet
	for _, a := range aggs {
		if a.Count > 1 {
			t.Errorf("venue1 bucket %s count %d, want 1", a.Bucket, a.Count)
		}
	}
}
func TestRepo_Overview_PnLZeroExcluded(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	t.Cleanup(pool.Close)
	repo := NewRepository(pool)
	venueID := testVenue(t, pool)
	addrZero := testAddr()
	addrNonZero := testAddr()
	cleanupAddrs(t, pool, venueID, addrZero, addrNonZero)

	for _, a := range []string{addrZero, addrNonZero} {
		if _, _, err := repo.UpsertRegistry(ctx, venueID, a, SourceLeaderboard, nil, nil); err != nil {
			t.Fatalf("registry: %v", err)
		}
	}
	zero := 0.0
	nonzero := 5000.0
	vol := 100.0
	tc := int64(2)
	lc, sc := int64(1), int64(1)
	for _, pair := range []struct {
		addr string
		pnl  float64
	}{
		{addrZero, zero},
		{addrNonZero, nonzero},
	} {
		p := pair.pnl
		m := &PeriodMetrics{VenueID: venueID, WalletAddress: pair.addr, Period: Period30D,
			AsOf: time.Now().UTC(), PnL: &p, RealizedPnL: &p, Volume: &vol,
			TradeCount: &tc, LongCount: &lc, ShortCount: &sc,
			DataStatus: DataReady, CalculationVersion: CurrentCalculationVersion}
		if err := repo.UpsertPeriodMetrics(ctx, m); err != nil {
			t.Fatalf("period: %v", err)
		}
	}

	us, err := repo.UniverseSize(ctx, venueID, Period30D)
	if err != nil || us != 2 {
		t.Errorf("universe: %d %v", us, err)
	}
	pn, err := repo.PnlWalletCount(ctx, venueID, Period30D, "realized_pnl")
	if err != nil || pn != 1 {
		t.Errorf("pnl wallets: %d %v", pn, err)
	}
	aggs, err := repo.AggregatePnLBuckets(ctx, venueID, Period30D, "realized_pnl")
	if err != nil {
		t.Fatalf("agg: %v", err)
	}
	total := 0
	for _, a := range aggs {
		total += a.Count
	}
	if total != 1 {
		t.Errorf("bucket wallet total: %d want 1", total)
	}
}

func TestRepo_Overview_BucketDetail(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	t.Cleanup(pool.Close)
	repo := NewRepository(pool)
	venueCode := fmt.Sprintf("trader-test-venue-%d", time.Now().UnixNano())
	if _, err := pool.Exec(ctx, `INSERT INTO venues (code, name, venue_type) VALUES ($1, 'Trader Test Venue', 'PERP_DEX')`, venueCode); err != nil {
		t.Fatalf("venue: %v", err)
	}
	var venueID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM venues WHERE code = $1`, venueCode).Scan(&venueID); err != nil {
		t.Fatalf("venue id: %v", err)
	}
	addr1 := testAddr()
	addr2 := testAddr()
	cleanupAddrs(t, pool, venueID, addr1, addr2)
	t.Cleanup(func() {
		cctx := context.Background()
		for _, a := range []string{addr1, addr2} {
			_, _ = pool.Exec(cctx, `DELETE FROM trader_daily_stats WHERE venue_id = $1 AND wallet_address = $2`, venueID, a)
			_, _ = pool.Exec(cctx, `DELETE FROM trader_trades WHERE venue_id = $1 AND wallet_address = $2`, venueID, a)
			_, _ = pool.Exec(cctx, `DELETE FROM trader_positions WHERE venue_id = $1 AND wallet_address = $2`, venueID, a)
			_, _ = pool.Exec(cctx, `DELETE FROM trader_position_summary WHERE venue_id = $1 AND wallet_address = $2`, venueID, a)
			_, _ = pool.Exec(cctx, `DELETE FROM trader_equity_daily WHERE venue_id = $1 AND wallet_address = $2`, venueID, a)
			_, _ = pool.Exec(cctx, `DELETE FROM trader_period_metrics WHERE venue_id = $1 AND wallet_address = $2`, venueID, a)
		}
	})

	for _, a := range []string{addr1, addr2} {
		if _, _, err := repo.UpsertRegistry(ctx, venueID, a, SourceLeaderboard, nil, nil); err != nil {
			t.Fatalf("registry: %v", err)
		}
	}

	// period metrics
	pnl1 := 1500000.0
	pnl2 := -50000.0
	vol1 := 100000.0
	vol2 := 20000.0
	tc1, tc2 := int64(10), int64(5)
	lc1, sc1 := int64(6), int64(4)
	lc2, sc2 := int64(2), int64(3)
	for i, a := range []string{addr1, addr2} {
		pnl := pnl1
		vol := vol1
		tc := tc1
		lc, sc := lc1, sc1
		if i == 1 {
			pnl, vol, tc, lc, sc = pnl2, vol2, tc2, lc2, sc2
		}
		m := &PeriodMetrics{VenueID: venueID, WalletAddress: a, Period: Period30D,
			AsOf: time.Now().UTC(), PnL: &pnl, RealizedPnL: &pnl, Volume: &vol,
			TradeCount: &tc, LongCount: &lc, ShortCount: &sc,
			DataStatus: DataReady, CalculationVersion: CurrentCalculationVersion}
		if err := repo.UpsertPeriodMetrics(ctx, m); err != nil {
			t.Fatalf("period: %v", err)
		}
	}

	// equity daily
	end1 := 6000000.0
	day0 := time.Now().UTC().Truncate(24 * time.Hour)
	if err := repo.UpsertEquityDaily(ctx, venueID, addr1, EquityDay{Date: day0, StartEquity: &end1, EndEquity: &end1, PeakEquity: &end1}); err != nil {
		t.Fatalf("eq1: %v", err)
	}
	end2 := 50000.0
	if err := repo.UpsertEquityDaily(ctx, venueID, addr2, EquityDay{Date: day0, StartEquity: &end2, EndEquity: &end2, PeakEquity: &end2}); err != nil {
		t.Fatalf("eq2: %v", err)
	}

	// positions
	snap1 := snapWith(Position{Coin: "BTC", Side: "LONG", Size: 0.5, EntryPrice: fptr(60000), MarkPrice: fptr(63000), PositionValue: fptr(31500)})
	snap1.AccountValue = fptr(6000000)
	snap1.TotalNtlPos = fptr(100000)
	if err := repo.ReplacePositions(ctx, venueID, addr1, snap1); err != nil {
		t.Fatalf("pos1: %v", err)
	}
	snap2 := snapWith(Position{Coin: "ETH", Side: "SHORT", Size: 2, EntryPrice: fptr(3000), MarkPrice: fptr(2900), PositionValue: fptr(50000)})
	snap2.AccountValue = fptr(50000)
	snap2.TotalNtlPos = fptr(50000)
	if err := repo.ReplacePositions(ctx, venueID, addr2, snap2); err != nil {
		t.Fatalf("pos2: %v", err)
	}

	// trades (closed today) for positioning/markets fallback
	tradeDay := time.Now().UTC().Truncate(24 * time.Hour)
	trades1 := []CompletedTrade{{Market: "BTC", Long: true, OpenTime: tradeDay.Add(1 * time.Hour), CloseTime: tradeDay.Add(2 * time.Hour), Volume: 20000, PnL: 1000, Fees: 10, Fills: 2}}
	trades2 := []CompletedTrade{{Market: "ETH", Long: false, OpenTime: tradeDay.Add(1 * time.Hour), CloseTime: tradeDay.Add(2 * time.Hour), Volume: 5000, PnL: -200, Fees: 5, Fills: 1}}
	if err := repo.ReplaceTradesForDay(ctx, venueID, addr1, tradeDay, trades1); err != nil {
		t.Fatalf("trades1: %v", err)
	}
	if err := repo.ReplaceTradesForDay(ctx, venueID, addr2, tradeDay, trades2); err != nil {
		t.Fatalf("trades2: %v", err)
	}

	// daily stats
	rpnl := 100.0
	ds := &DailyStats{VenueID: venueID, WalletAddress: addr1, StatDate: day0, RealizedPnL: &rpnl, TradeCount: 1, LongCount: 1, ShortCount: 0}
	if err := repo.UpsertDailyStats(ctx, ds); err != nil {
		t.Fatalf("daily: %v", err)
	}

	// BucketWalletAddresses pnl
	ws, err := repo.BucketWalletAddresses(ctx, venueID, "pnl", "extremely_profitable", Period30D)
	if err != nil {
		t.Fatalf("bwa pnl: %v", err)
	}
	if len(ws) != 1 || ws[0] != addr1 {
		t.Errorf("pnl bucket wallets: %v want [%s]", ws, addr1)
	}
	// account kind path (critical)
	ws2, err := repo.BucketWalletAddresses(ctx, venueID, "account", "apex", Period30D)
	if err != nil {
		t.Fatalf("bwa account: %v", err)
	}
	if len(ws2) != 1 || ws2[0] != addr1 {
		t.Errorf("account bucket wallets: %v want [%s]", ws2, addr1)
	}
	// unprofitable pnl bucket
	ws3, err := repo.BucketWalletAddresses(ctx, venueID, "pnl", "unprofitable", Period30D)
	if err != nil {
		t.Fatalf("bwa unprofitable: %v", err)
	}
	if len(ws3) != 1 || ws3[0] != addr2 {
		t.Errorf("unprofitable bucket wallets: %v", ws3)
	}
	// medium account bucket
	ws4, err := repo.BucketWalletAddresses(ctx, venueID, "account", "medium", Period30D)
	if err != nil {
		t.Fatalf("bwa medium: %v", err)
	}
	if len(ws4) != 1 || ws4[0] != addr2 {
		t.Errorf("medium bucket wallets: %v", ws4)
	}

	// BucketPeriodAgg
	lc, sc, profit, loss, volume, err := repo.BucketPeriodAgg(ctx, venueID, Period30D, []string{addr1})
	if err != nil {
		t.Fatalf("period agg: %v", err)
	}
	if lc != lc1 || sc != sc1 {
		t.Errorf("counts: %d %d", lc, sc)
	}
	if profit != pnl1 {
		t.Errorf("profit: %v", profit)
	}
	if volume != vol1 {
		t.Errorf("volume: %v", volume)
	}
	_ = loss

	// BucketPositionNotional
	longN, shortN, n, err := repo.BucketPositionNotional(ctx, venueID, []string{addr1, addr2})
	if err != nil {
		t.Fatalf("pos notional: %v", err)
	}
	if longN <= 0 || shortN <= 0 || n != 2 {
		t.Errorf("pos notional: %v %v %d", longN, shortN, n)
	}

	// BucketCoverage
	posC, tradeC, err := repo.BucketCoverage(ctx, venueID, []string{addr1, addr2})
	if err != nil {
		t.Fatalf("coverage: %v", err)
	}
	if posC != 2 || tradeC != 2 {
		t.Errorf("coverage: pos=%d trades=%d", posC, tradeC)
	}

	// BucketPositioning
	since := time.Now().UTC().AddDate(0, 0, -1).Truncate(24 * time.Hour)
	days, err := repo.BucketPositioning(ctx, venueID, []string{addr1, addr2}, since)
	if err != nil {
		t.Fatalf("positioning: %v", err)
	}
	if len(days) == 0 {
		t.Error("positioning empty")
	}

	// BucketWallets (pnl kind)
	rows, total, err := repo.BucketWallets(ctx, venueID, "pnl", "extremely_profitable", Period30D, day0, "equity", "desc", 1, 10)
	if err != nil {
		t.Fatalf("bucket wallets: %v", err)
	}
	if total != 1 || len(rows) != 1 {
		t.Errorf("bucket wallets: total=%d len=%d", total, len(rows))
	}
	if rows[0].Address != addr1 {
		t.Errorf("bucket wallets addr: %s", rows[0].Address)
	}
	// account kind path
	arows, atotal, err := repo.BucketWallets(ctx, venueID, "account", "apex", Period30D, day0, "equity", "desc", 1, 10)
	if err != nil {
		t.Fatalf("bucket wallets account: %v", err)
	}
	if atotal != 1 || len(arows) != 1 {
		t.Errorf("account bucket wallets: total=%d len=%d", atotal, len(arows))
	}

	// BucketMarkets
	mrows, mtotal, err := repo.BucketMarkets(ctx, venueID, []string{addr1, addr2}, day0, "", "notional", "desc", 1, 10)
	if err != nil {
		t.Fatalf("bucket markets: %v", err)
	}
	if mtotal == 0 || len(mrows) == 0 {
		t.Errorf("bucket markets: total=%d len=%d", mtotal, len(mrows))
	}

	// venue isolation
	otherVenueCode := fmt.Sprintf("trader-test-venue-%d", time.Now().UnixNano())
	if _, err := pool.Exec(ctx, `INSERT INTO venues (code, name, venue_type) VALUES ($1, 'Other', 'PERP_DEX')`, otherVenueCode); err != nil {
		t.Fatalf("other venue: %v", err)
	}
	var otherVenueID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM venues WHERE code = $1`, otherVenueCode).Scan(&otherVenueID); err != nil {
		t.Fatalf("other venue id: %v", err)
	}
	wsOther, err := repo.BucketWalletAddresses(ctx, otherVenueID, "pnl", "extremely_profitable", Period30D)
	if err != nil {
		t.Fatalf("other venue bwa: %v", err)
	}
	if len(wsOther) != 0 {
		t.Errorf("venue isolation failed: %v", wsOther)
	}

	// period isolation
	wsPeriod, err := repo.BucketWalletAddresses(ctx, venueID, "pnl", "extremely_profitable", Period7D)
	if err != nil {
		t.Fatalf("period isolation bwa: %v", err)
	}
	if len(wsPeriod) != 0 {
		t.Errorf("period isolation failed: %v", wsPeriod)
	}
}
