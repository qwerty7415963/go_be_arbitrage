package trader

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/hyperliquid"
)

type parallelPerfFake struct {
	portfolioStarted chan struct{}
	fillsStarted     chan struct{}
	release          chan struct{}

	portfolioCalls atomic.Int64
	fillsCalls     atomic.Int64

	portfolio map[string][]hyperliquid.PortfolioPoint
	raw       []hyperliquid.Fill
	truncated bool
}

func (f *parallelPerfFake) FetchClearinghouseState(_ context.Context, _ string, _ ...string) (*hyperliquid.ClearinghouseState, error) {
	return nil, nil
}
func (f *parallelPerfFake) FetchPerpDexs(_ context.Context) ([]string, error) { return nil, nil }
func (f *parallelPerfFake) FetchSpotState(_ context.Context, _ string) (*hyperliquid.SpotState, error) {
	return nil, nil
}
func (f *parallelPerfFake) FetchUserFills(_ context.Context, _ string) ([]hyperliquid.Fill, error) {
	return nil, nil
}
func (f *parallelPerfFake) FetchOpenOrders(_ context.Context, _ string) ([]hyperliquid.OpenOrder, error) {
	return nil, nil
}
func (f *parallelPerfFake) FetchHistoricalOrders(_ context.Context, _ string) ([]hyperliquid.HistoricalOrder, error) {
	return nil, nil
}
func (f *parallelPerfFake) FetchLedgerUpdates(_ context.Context, _ string, _ int64) ([]hyperliquid.LedgerUpdate, error) {
	return nil, nil
}
func (f *parallelPerfFake) FetchFillsWindow(_ context.Context, _ string, _, _ int64) ([]hyperliquid.Fill, bool, error) {
	f.fillsCalls.Add(1)
	select {
	case f.fillsStarted <- struct{}{}:
	default:
	}
	select {
	case <-f.portfolioStarted:
	case <-time.After(2 * time.Second):
	}
	select {
	case <-f.release:
	case <-time.After(2 * time.Second):
	}
	if f.raw == nil {
		return []hyperliquid.Fill{}, f.truncated, nil
	}
	return f.raw, f.truncated, nil
}
func (f *parallelPerfFake) FetchUserFunding(_ context.Context, _ string, _, _ int64) ([]hyperliquid.FundingUpdate, error) {
	return []hyperliquid.FundingUpdate{}, nil
}
func (f *parallelPerfFake) FetchPortfolio(_ context.Context, _ string) (map[string][]hyperliquid.PortfolioPoint, error) {
	f.portfolioCalls.Add(1)
	select {
	case f.portfolioStarted <- struct{}{}:
	default:
	}
	select {
	case <-f.fillsStarted:
	case <-time.After(2 * time.Second):
	}
	select {
	case <-f.release:
	case <-time.After(2 * time.Second):
	}
	if f.portfolio == nil {
		return map[string][]hyperliquid.PortfolioPoint{}, nil
	}
	return f.portfolio, nil
}

func TestLivePerformance_ParallelFetch_StartsBothBeforeEitherCompletes(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	fake := &parallelPerfFake{
		portfolioStarted: make(chan struct{}, 1),
		fillsStarted:     make(chan struct{}, 1),
		release:          make(chan struct{}),
		portfolio:        map[string][]hyperliquid.PortfolioPoint{},
		raw: []hyperliquid.Fill{
			rawFillForUniverse("BTC", "B", "1", "50000", "0", "1", now.Add(-time.Hour), 1),
			rawFillForUniverse("BTC", "A", "1", "51000", "100", "1", now.Add(-30*time.Minute), 2),
		},
	}
	s := universeService(fake, LiveCacheTTL)
	venue := "hyperliquid"
	addr := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	done := make(chan error, 1)
	go func() {
		_, err := fetchLivePerformance(context.Background(), s, uuid.New(), addr, venue, Period7D, now)
		done <- err
	}()

	deadline := time.Now().Add(2 * time.Second)
	for !(fake.portfolioCalls.Load() >= 1 && fake.fillsCalls.Load() >= 1) {
		if time.Now().After(deadline) {
			t.Fatalf("parallel fetch did not start both; portfolioCalls=%d fillsCalls=%d", fake.portfolioCalls.Load(), fake.fillsCalls.Load())
		}
		time.Sleep(10 * time.Millisecond)
	}

	close(fake.release)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("fetchLivePerformance: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("fetchLivePerformance did not complete")
	}

	if got := fake.portfolioCalls.Load(); got != 1 {
		t.Fatalf("portfolio calls = %d, want 1", got)
	}
	if got := fake.fillsCalls.Load(); got != 1 {
		t.Fatalf("fills calls = %d, want 1", got)
	}
}
