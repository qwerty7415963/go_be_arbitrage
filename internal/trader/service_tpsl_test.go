//go:build integration

package trader

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/qwerty7415963/go_be_arbitrage/internal/hyperliquid"
)

type tpslFake struct {
	state  *hyperliquid.ClearinghouseState
	orders []hyperliquid.OpenOrder
	dexes  []string
}

func (f *tpslFake) FetchClearinghouseState(_ context.Context, _ string, _ ...string) (*hyperliquid.ClearinghouseState, error) {
	return f.state, nil
}
func (f *tpslFake) FetchPerpDexs(_ context.Context) ([]string, error) { return f.dexes, nil }
func (f *tpslFake) FetchSpotState(_ context.Context, _ string) (*hyperliquid.SpotState, error) {
	return &hyperliquid.SpotState{}, nil
}
func (f *tpslFake) FetchUserFills(_ context.Context, _ string) ([]hyperliquid.Fill, error) {
	return nil, nil
}
func (f *tpslFake) FetchOpenOrders(_ context.Context, _ string, _ ...string) ([]hyperliquid.OpenOrder, error) {
	return nil, nil
}
func (f *tpslFake) FetchHistoricalOrders(_ context.Context, _ string) ([]hyperliquid.HistoricalOrder, error) {
	return nil, nil
}
func (f *tpslFake) FetchLedgerUpdates(_ context.Context, _ string, _ int64) ([]hyperliquid.LedgerUpdate, error) {
	return nil, nil
}
func (f *tpslFake) FetchFillsWindow(_ context.Context, _ string, _, _ int64) ([]hyperliquid.Fill, bool, error) {
	return nil, false, nil
}
func (f *tpslFake) FetchUserFunding(_ context.Context, _ string, _, _ int64) ([]hyperliquid.FundingUpdate, error) {
	return nil, nil
}
func (f *tpslFake) FetchPortfolio(_ context.Context, _ string) (map[string]hyperliquid.PortfolioSeries, error) {
	return nil, nil
}
func (f *tpslFake) FetchFrontendOpenOrders(_ context.Context, _ string, _ ...string) ([]hyperliquid.OpenOrder, error) {
	return f.orders, nil
}

func mustState(t *testing.T, raw string) *hyperliquid.ClearinghouseState {
	t.Helper()
	var st hyperliquid.ClearinghouseState
	if err := json.Unmarshal([]byte(raw), &st); err != nil {
		t.Fatal(err)
	}
	return &st
}

func TestService_Positions_TPSL_Attach(t *testing.T) {
	pool := testPool(t)
	t.Cleanup(pool.Close)
	repo := NewRepository(pool)
	venueID := testVenue(t, pool)
	addr := testAddr()
	ctx := context.Background()
	if _, _, err := repo.UpsertRegistry(ctx, venueID, addr, SourceLeaderboard, nil, nil); err != nil {
		t.Fatalf("registry: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM trader_registry WHERE venue_id=$1 AND wallet_address=$2`, venueID, addr)
	})

	fake := &tpslFake{
		state: mustState(t, `{"marginSummary":{"accountValue":"1000","totalNtlPos":"500","totalMarginUsed":"100"},"assetPositions":[{"type":"oneWay","position":{"coin":"BTC","szi":"1","entryPx":"100","positionValue":"100","unrealizedPnl":"0","returnOnEquity":"0","maxLeverage":"10","marginUsed":"10"}}]}`),
		orders: []hyperliquid.OpenOrder{
			{Coin: "BTC", Side: "A", ReduceOnly: true, OrderType: "Take Profit Market", TriggerPx: "110"},
			{Coin: "BTC", Side: "A", ReduceOnly: true, OrderType: "Stop Market", TriggerPx: "90"},
		},
	}
	svc := NewService(repo, nil, []byte("tpsl-test-secret-32-bytes!!!!!!")).WithOnDemand(fake, NewOnDemandCache(LiveCacheTTL))
	got, err := svc.Positions(ctx, "trader-test-venue", addr, "coin", "asc")
	if err != nil {
		t.Fatalf("positions: %v", err)
	}
	if len(got.Positions) != 1 {
		t.Fatalf("positions: %+v", got.Positions)
	}
	p := got.Positions[0]
	if len(p.TpPxs) != 1 || p.TpPxs[0] != 110 {
		t.Errorf("TpPxs: %v", p.TpPxs)
	}
	if len(p.SlPxs) != 1 || p.SlPxs[0] != 90 {
		t.Errorf("SlPxs: %v", p.SlPxs)
	}
}

type tpslErrFake struct {
	inner *tpslFake
}

func (f *tpslErrFake) FetchClearinghouseState(ctx context.Context, addr string, dex ...string) (*hyperliquid.ClearinghouseState, error) {
	return f.inner.FetchClearinghouseState(ctx, addr, dex...)
}
func (f *tpslErrFake) FetchPerpDexs(ctx context.Context) ([]string, error) { return f.inner.FetchPerpDexs(ctx) }
func (f *tpslErrFake) FetchSpotState(ctx context.Context, addr string) (*hyperliquid.SpotState, error) {
	return f.inner.FetchSpotState(ctx, addr)
}
func (f *tpslErrFake) FetchUserFills(ctx context.Context, addr string) ([]hyperliquid.Fill, error) {
	return f.inner.FetchUserFills(ctx, addr)
}
func (f *tpslErrFake) FetchOpenOrders(ctx context.Context, addr string, dex ...string) ([]hyperliquid.OpenOrder, error) {
	return f.inner.FetchOpenOrders(ctx, addr, dex...)
}
func (f *tpslErrFake) FetchHistoricalOrders(ctx context.Context, addr string) ([]hyperliquid.HistoricalOrder, error) {
	return f.inner.FetchHistoricalOrders(ctx, addr)
}
func (f *tpslErrFake) FetchLedgerUpdates(ctx context.Context, addr string, ms int64) ([]hyperliquid.LedgerUpdate, error) {
	return f.inner.FetchLedgerUpdates(ctx, addr, ms)
}
func (f *tpslErrFake) FetchFillsWindow(ctx context.Context, addr string, a, b int64) ([]hyperliquid.Fill, bool, error) {
	return f.inner.FetchFillsWindow(ctx, addr, a, b)
}
func (f *tpslErrFake) FetchUserFunding(ctx context.Context, addr string, a, b int64) ([]hyperliquid.FundingUpdate, error) {
	return f.inner.FetchUserFunding(ctx, addr, a, b)
}
func (f *tpslErrFake) FetchPortfolio(ctx context.Context, addr string) (map[string]hyperliquid.PortfolioSeries, error) {
	return f.inner.FetchPortfolio(ctx, addr)
}
func (f *tpslErrFake) FetchFrontendOpenOrders(ctx context.Context, addr string, dex ...string) ([]hyperliquid.OpenOrder, error) {
	return nil, context.DeadlineExceeded
}

func TestService_Positions_TPSL_FrontendErrorNonFatal(t *testing.T) {
	pool := testPool(t)
	t.Cleanup(pool.Close)
	repo := NewRepository(pool)
	venueID := testVenue(t, pool)
	addr := testAddr()
	ctx := context.Background()
	if _, _, err := repo.UpsertRegistry(ctx, venueID, addr, SourceLeaderboard, nil, nil); err != nil {
		t.Fatalf("registry: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM trader_registry WHERE venue_id=$1 AND wallet_address=$2`, venueID, addr)
	})
	fake := &tpslFake{
		state: mustState(t, `{"marginSummary":{"accountValue":"1000"},"assetPositions":[{"type":"oneWay","position":{"coin":"BTC","szi":"1","entryPx":"100","positionValue":"100"}}]}`),
	}
	errFake := &tpslErrFake{inner: fake}
	svc := NewService(repo, nil, []byte("tpsl-test-secret-32-bytes!!!!!!")).WithOnDemand(errFake, NewOnDemandCache(LiveCacheTTL))
	got, err := svc.Positions(ctx, "trader-test-venue", addr, "coin", "asc")
	if err != nil {
		t.Fatalf("positions: %v", err)
	}
	if got.DataStatus != DataReady {
		t.Errorf("want ready, got %s", got.DataStatus)
	}
}