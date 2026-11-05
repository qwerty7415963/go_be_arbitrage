//go:build integration

package trader

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/qwerty7415963/go_be_arbitrage/internal/hyperliquid"
)

// LIVE-U-25: Detail + Activity + Performance concurrent share one 30d
// userFillsByTime window fetch; responses stay consistent with the legacy
// per-endpoint fetch behavior (same trade reconstructed in all three).
func TestRepo_LiveUniverse_SharedFetch(t *testing.T) {
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

	now := time.Now().UTC().Truncate(time.Millisecond)
	fake := &countingUniverseFake{
		raw: []hyperliquid.Fill{
			rawFillForUniverse("BTC", "B", "1", "50000", "0", "2", now.Add(-2*time.Hour), 11),
			rawFillForUniverse("BTC", "A", "1", "51000", "900", "2", now.Add(-time.Hour), 12),
		},
		delay: 150 * time.Millisecond,
	}
	svc := NewService(repo, nil, []byte("universe-integration-secret-32b!!"))
	svc.WithOnDemand(fake, NewOnDemandCache(LiveCacheTTL))

	var wg sync.WaitGroup
	start := make(chan struct{})
	var detail *Detail
	var detailErr error
	var act *ActivityPage
	var actErr error
	var perf *PerformanceDTO
	var perfErr error

	wg.Add(3)
	go func() {
		defer wg.Done()
		<-start
		detail, detailErr = svc.Detail(ctx, testVenueCode, addr, Period30D)
	}()
	go func() {
		defer wg.Done()
		<-start
		act, actErr = svc.Activity(ctx, testVenueCode, addr, ActivityQuery{Limit: 20})
	}()
	go func() {
		defer wg.Done()
		<-start
		perf, perfErr = svc.Performance(ctx, testVenueCode, addr, Period30D, PerformanceModePerps, false)
	}()
	close(start)
	wg.Wait()

	if detailErr != nil {
		t.Fatalf("detail: %v", detailErr)
	}
	if actErr != nil {
		t.Fatalf("activity: %v", actErr)
	}
	if perfErr != nil {
		t.Fatalf("performance: %v", perfErr)
	}
	if got := fake.fillsCalls.Load(); got != 1 {
		t.Fatalf("Detail+Activity+Performance concurrent must cost 1 window fetch, got %d", got)
	}
	// Behavior parity with the legacy per-endpoint fetches: one BTC cycle
	// visible in all three payloads, ready + not partial, as_of shared.
	if act.DataStatus != DataReady || act.Partial {
		t.Fatalf("activity: want ready+partial=false, got %+v", act)
	}
	if len(act.Rows) != 1 || act.Rows[0].Market != "BTC" {
		t.Fatalf("activity rows: %+v", act.Rows)
	}
	if act.Counts.Total != 1 {
		t.Fatalf("activity counts: %+v", act.Counts)
	}
	if perf.Metrics == nil || perf.Metrics.TradeCount == nil || *perf.Metrics.TradeCount != 1 {
		t.Fatalf("performance metrics: %+v", perf.Metrics)
	}
	if perf.Metrics.DataStatus != DataReady || perf.Metrics.IsPartial {
		t.Fatalf("performance status: %+v", perf.Metrics)
	}
	if detail.Metrics == nil || detail.Metrics.TradeCount == nil || *detail.Metrics.TradeCount != 1 {
		t.Fatalf("detail metrics: %+v", detail.Metrics)
	}
	if detail.DataStatus != DataReady {
		t.Fatalf("detail status: %v", detail.DataStatus)
	}
	if act.AsOf == nil || perf.Metrics.MetricsAsOf == nil || detail.AsOf == nil {
		t.Fatal("as_of must be set (universe fetch time)")
	}
	if !act.AsOf.Equal(*perf.Metrics.MetricsAsOf) || !act.AsOf.Equal(*detail.AsOf) {
		t.Fatalf("as_of must be the shared universe fetch time: activity=%v perf=%v detail=%v",
			act.AsOf, perf.Metrics.MetricsAsOf, detail.AsOf)
	}
	if *perf.Metrics.PnL != *detail.Metrics.PnL {
		t.Fatalf("detail must reuse performance universe: perf=%v detail=%v",
			*perf.Metrics.PnL, *detail.Metrics.PnL)
	}
}

// PS-I-01/02: performance cache is keyed by mode; switching modes refetches
// once, same-mode repeats within TTL stay free, and all four ranges are set.
func TestRepo_PerformanceSeries_ModeCacheSeparation(t *testing.T) {
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

	now := time.Now().UTC().Truncate(time.Millisecond)
	fake := &countingUniverseFake{
		raw: []hyperliquid.Fill{
			rawFillForUniverse("BTC", "B", "1", "50000", "0", "1", now.Add(-time.Hour), 21),
			rawFillForUniverse("BTC", "A", "1", "51000", "500", "1", now.Add(-30*time.Minute), 22),
		},
		portfolio: map[string]hyperliquid.PortfolioSeries{
			"day": {
				AccountValue: []hyperliquid.PortfolioPoint{{Time: now.Add(-time.Hour).UnixMilli(), Value: "111"}},
				PnL:          []hyperliquid.PortfolioPoint{{Time: now.Add(-time.Hour).UnixMilli(), Value: "1.5"}},
			},
			"perpDay": {
				AccountValue: []hyperliquid.PortfolioPoint{{Time: now.Add(-time.Hour).UnixMilli(), Value: "222"}},
				PnL:          []hyperliquid.PortfolioPoint{{Time: now.Add(-time.Hour).UnixMilli(), Value: "2.5"}},
			},
		},
	}
	svc := NewService(repo, nil, []byte("series-integration-secret-32b!"))
	svc.WithOnDemand(fake, NewOnDemandCache(LiveCacheTTL))

	perps, err := svc.Performance(ctx, testVenueCode, addr, Period30D, PerformanceModePerps, false)
	if err != nil {
		t.Fatalf("perps: %v", err)
	}
	if perps.Mode != PerformanceModePerps || len(perps.Series) != len(performanceRangeKeys) {
		t.Fatalf("perps shape: %+v", perps)
	}
	if got := perps.Series["24H"].AccountValue; len(got) != 1 || got[0].Value != 222 {
		t.Fatalf("perps 24H must use perpDay: %+v", got)
	}
	if got := fake.portfolioCalls.Load(); got != 1 {
		t.Fatalf("perps portfolio calls = %d, want 1", got)
	}

	combined, err := svc.Performance(ctx, testVenueCode, addr, Period30D, PerformanceModeCombined, false)
	if err != nil {
		t.Fatalf("combined: %v", err)
	}
	if combined.Mode != PerformanceModeCombined {
		t.Fatalf("combined mode: %+v", combined.Mode)
	}
	if got := combined.Series["24H"].AccountValue; len(got) != 1 || got[0].Value != 111 {
		t.Fatalf("combined 24H must use day: %+v", got)
	}
	if got := fake.portfolioCalls.Load(); got != 2 {
		t.Fatalf("mode switch must refetch once: calls=%d", got)
	}

	again, err := svc.Performance(ctx, testVenueCode, addr, Period30D, PerformanceModePerps, false)
	if err != nil {
		t.Fatalf("perps again: %v", err)
	}
	if got := again.Series["24H"].AccountValue; len(got) != 1 || got[0].Value != 222 {
		t.Fatalf("perps cache hit series: %+v", got)
	}
	if got := fake.portfolioCalls.Load(); got != 2 {
		t.Fatalf("same-mode TTL hit must not refetch: calls=%d", got)
	}
}
