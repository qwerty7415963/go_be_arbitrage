//go:build integration

package trader

import (
	"context"
	"testing"
	"time"
)

type fakePortfolio struct {
	data  map[string][]EquityPoint
	err   error
	calls int
}

func (f *fakePortfolio) FetchPortfolio(_ context.Context, _ string) (map[string][]EquityPoint, error) {
	f.calls++
	return f.data, f.err
}

// Live finding (SQLSTATE 22003): dust-to-millions equity curves overflowed
// NUMERIC(20,12). Wide columns must accept extreme-but-real values.
func TestRepo_WideNumerics(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	t.Cleanup(pool.Close)
	repo := NewRepository(pool)
	venueID := testVenue(t, pool)
	addr := testAddr()
	cleanupAddrs(t, pool, venueID, addr)
	if _, _, err := repo.UpsertRegistry(ctx, venueID, addr,
		SourceLeaderboard, nil, nil); err != nil {
		t.Fatalf("registry: %v", err)
	}

	huge := 1234567890123.45
	day := EquityDay{Date: time.Now().UTC().Truncate(24 * time.Hour),
		StartEquity: ptr(0.01), EndEquity: ptr(1000000), PeakEquity: ptr(1000000)}
	day.DailyReturn = &huge
	if err := repo.UpsertEquityDaily(ctx, venueID, addr, day); err != nil {
		t.Fatalf("equity wide: %v", err)
	}
	pf := 9876543210987.65
	m := &PeriodMetrics{VenueID: venueID, WalletAddress: addr, Period: Period30D,
		AsOf: time.Now().UTC(), ProfitFactor: &pf,
		DataStatus: DataReady, CalculationVersion: CurrentCalculationVersion}
	if err := repo.UpsertPeriodMetrics(ctx, m); err != nil {
		t.Fatalf("period wide PF: %v", err)
	}
	got, err := repo.GetPeriodMetrics(ctx, venueID, addr, Period30D)
	if err != nil || got.ProfitFactor == nil || *got.ProfitFactor != pf {
		t.Errorf("roundtrip: %+v %v", got, err)
	}
}

func ptr(v float64) *float64 { return &v }

// EQ-I-01/02 (BE-021/022): equity curve stored per day; period drawdown
// computed from the curve (100→120→90 = 25%).
func TestSync_EquityDrawdown(t *testing.T) {
	now := time.Now().UTC()
	d0 := now.Truncate(24 * time.Hour)
	fetch := &fakeFills{fills: dayFills(now)}
	repo, svc, addr := syncFixture(t, fetch)
	svc.WithPortfolio(&fakePortfolio{data: map[string][]EquityPoint{
		"day": {
			{Time: d0.Add(-48 * time.Hour), Value: 100},
			{Time: d0.Add(-24 * time.Hour), Value: 120},
			{Time: d0, Value: 90},
		},
	}})
	ctx := context.Background()
	venueID, _ := repo.VenueIDByCode(ctx, testVenueCode)

	if err := svc.SyncWallet(ctx, addr, now); err != nil {
		t.Fatalf("sync: %v", err)
	}
	curve, err := repo.ListEquityDaily(ctx, venueID, addr,
		now.Add(-72*time.Hour), now)
	if err != nil || len(curve) != 3 {
		t.Fatalf("equity rows: %v %d", err, len(curve))
	}
	st, _ := repo.GetSyncState(ctx, venueID, addr)
	if st.LastPortfolioSyncAt == nil {
		t.Error("last_portfolio_sync_at must be set")
	}
	m, _ := repo.GetPeriodMetrics(ctx, venueID, addr, Period30D)
	if m.MaxDrawdownPct == nil || *m.MaxDrawdownPct != 25.0 {
		t.Errorf("drawdown: %+v", m.MaxDrawdownPct)
	}
}
