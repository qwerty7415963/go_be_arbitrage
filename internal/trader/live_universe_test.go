package trader

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/hyperliquid"
)

// countingUniverseFake counts window fetches; other seams stubbed.
type countingUniverseFake struct {
	fillsCalls     atomic.Int64
	fundingCalls   atomic.Int64
	portfolioCalls atomic.Int64
	raw            []hyperliquid.Fill
	truncated      bool
	fillsErr       error
	funding        []hyperliquid.FundingUpdate
	portfolio      map[string][]hyperliquid.PortfolioPoint
	delay          time.Duration
}

func (f *countingUniverseFake) FetchClearinghouseState(_ context.Context, _ string) (*hyperliquid.ClearinghouseState, error) {
	return nil, errors.New("not used")
}

func (f *countingUniverseFake) FetchSpotState(_ context.Context, _ string) (*hyperliquid.SpotState, error) {
	return nil, errors.New("not used")
}

func (f *countingUniverseFake) FetchUserFills(_ context.Context, _ string) ([]hyperliquid.Fill, error) {
	return nil, errors.New("not used")
}

func (f *countingUniverseFake) FetchOpenOrders(_ context.Context, _ string) ([]hyperliquid.OpenOrder, error) {
	return nil, errors.New("not used")
}

func (f *countingUniverseFake) FetchHistoricalOrders(_ context.Context, _ string) ([]hyperliquid.HistoricalOrder, error) {
	return nil, errors.New("not used")
}

func (f *countingUniverseFake) FetchLedgerUpdates(_ context.Context, _ string, _ int64) ([]hyperliquid.LedgerUpdate, error) {
	return nil, errors.New("not used")
}

func (f *countingUniverseFake) FetchFillsWindow(_ context.Context, _ string, _, _ int64) ([]hyperliquid.Fill, bool, error) {
	f.fillsCalls.Add(1)
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	if f.fillsErr != nil {
		return nil, false, f.fillsErr
	}
	return f.raw, f.truncated, nil
}

func (f *countingUniverseFake) FetchUserFunding(_ context.Context, _ string, _, _ int64) ([]hyperliquid.FundingUpdate, error) {
	f.fundingCalls.Add(1)
	if f.funding == nil {
		return []hyperliquid.FundingUpdate{}, nil
	}
	return f.funding, nil
}

func (f *countingUniverseFake) FetchPortfolio(_ context.Context, _ string) (map[string][]hyperliquid.PortfolioPoint, error) {
	f.portfolioCalls.Add(1)
	if f.portfolio == nil {
		return map[string][]hyperliquid.PortfolioPoint{}, nil
	}
	return f.portfolio, nil
}

func rawFillForUniverse(coin, side, sz, px, pnl, fee string, t time.Time, tid int64) hyperliquid.Fill {
	return hyperliquid.Fill{
		Coin: coin, Side: side, Sz: sz, Px: px, ClosedPnl: pnl, Fee: fee,
		Time: t.UnixMilli(), Tid: tid,
	}
}

func universeService(fake OnDemandClient, ttl time.Duration) *Service {
	s := NewService(nil, nil, []byte("universe-test-secret-32bytes!!"))
	if ttl <= 0 {
		ttl = LiveCacheTTL
	}
	return s.WithOnDemand(fake, NewOnDemandCache(ttl))
}

// LIVE-U-20: 3 concurrent universe consumers share exactly 1 upstream fetch.
func TestUniverse_Sharing_SingleFetch(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	fake := &countingUniverseFake{
		raw: []hyperliquid.Fill{
			rawFillForUniverse("BTC", "B", "1", "50000", "0", "1", now.Add(-time.Hour), 1),
			rawFillForUniverse("BTC", "A", "1", "51000", "1000", "1", now.Add(-30*time.Minute), 2),
		},
		delay: 120 * time.Millisecond,
	}
	s := universeService(fake, LiveCacheTTL)
	venue := "hyperliquid"
	addr := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	var wg sync.WaitGroup
	errs := make([]error, 3)
	unis := make([]*FillsUniverse, 3)
	start := make(chan struct{})
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			u, err := getFillsUniverse(context.Background(), s, venue, addr, now)
			unis[idx] = u
			errs[idx] = err
		}(i)
	}
	close(start)
	wg.Wait()
	for i := range errs {
		if errs[i] != nil {
			t.Fatalf("consumer %d: %v", i, errs[i])
		}
		if unis[i] == nil || len(unis[i].Fills) != 2 {
			t.Fatalf("consumer %d: want 2 fills, got %+v", i, unis[i])
		}
	}
	if got := fake.fillsCalls.Load(); got != 1 {
		t.Fatalf("concurrent universe must cost 1 upstream fetch, got %d", got)
	}
	if !unis[0].FetchedAt.Equal(now.UTC()) {
		t.Fatalf("FetchedAt must be request now: %v", unis[0].FetchedAt)
	}
}

// LIVE-U-21: sliceUniverse cuts 1D/7D/30D correctly, boundaries inclusive.
func TestSliceUniverse_Windows(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	mk := func(d time.Duration, tid int64) Fill {
		return Fill{Market: "BTC", Tid: tid, FilledAt: now.Add(-d).UTC(), Buy: true, Quantity: 1, Price: 50000}
	}
	rows := []Fill{
		mk(12*time.Hour, 1),   // in 1D
		mk(3*24*time.Hour, 2), // in 7D
		mk(10*24*time.Hour, 3),
		mk(20*24*time.Hour, 4),
		mk(29*24*time.Hour, 5), // in 30D
	}
	if got := sliceUniverse(rows, now, 24*time.Hour); len(got) != 1 || got[0].Tid != 1 {
		t.Fatalf("1D: want [1], got %v", tids(got))
	}
	if got := sliceUniverse(rows, now, 7*24*time.Hour); len(got) != 2 {
		t.Fatalf("7D: want 2, got %v", tids(got))
	}
	if got := sliceUniverse(rows, now, 30*24*time.Hour); len(got) != 5 {
		t.Fatalf("30D: want 5, got %v", tids(got))
	}
	// Boundary inclusive: exactly at cutoff stays.
	edge := []Fill{mk(7*24*time.Hour, 9)}
	if got := sliceUniverse(edge, now, 7*24*time.Hour); len(got) != 1 {
		t.Fatalf("boundary must be inclusive, got %v", tids(got))
	}
	// Future rows excluded; empty/zero-window empty.
	future := append(append([]Fill{}, rows...), Fill{Market: "BTC", Tid: 99, FilledAt: now.Add(time.Hour).UTC(), Buy: true, Quantity: 1, Price: 1})
	if got := sliceUniverse(future, now, 30*24*time.Hour); len(got) != 5 {
		t.Fatalf("future must be excluded, got %v", tids(got))
	}
	if got := sliceUniverse(nil, now, 24*time.Hour); len(got) != 0 {
		t.Fatalf("nil must slice to empty, got %v", got)
	}
	if got := sliceUniverse(rows, now, 0); len(got) != 0 {
		t.Fatalf("zero window must be empty, got %v", got)
	}
}

func tids(fills []Fill) []int64 {
	out := make([]int64, 0, len(fills))
	for _, f := range fills {
		out = append(out, f.Tid)
	}
	return out
}

// LIVE-U-22: truncated universes propagate partial:true without refill;
// as_of is the universe fetch time for both consumers.
func TestUniverse_TruncatedPropagates(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	fake := &countingUniverseFake{
		raw: []hyperliquid.Fill{
			rawFillForUniverse("BTC", "B", "1", "50000", "0", "1", now.Add(-2*time.Hour), 1),
			rawFillForUniverse("BTC", "A", "1", "51000", "100", "1", now.Add(-time.Hour), 2),
		},
		truncated: true,
	}
	s := universeService(fake, LiveCacheTTL)
	venue := "hyperliquid"
	addr := "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	u, err := getFillsUniverse(context.Background(), s, venue, addr, now)
	if err != nil {
		t.Fatalf("universe: %v", err)
	}
	if !u.Truncated {
		t.Fatal("universe must preserve truncated")
	}
	act, err := fetchLiveActivity(context.Background(), s, venue, addr, now)
	if err != nil {
		t.Fatalf("activity: %v", err)
	}
	if !act.Partial {
		t.Error("activity must emit partial:true on truncated universe")
	}
	if !act.AsOf.Equal(u.FetchedAt.UTC()) {
		t.Errorf("activity as_of must be universe fetch time: %v vs %v", act.AsOf, u.FetchedAt)
	}
	perf, err := fetchLivePerformance(context.Background(), s, uuid.New(), addr, venue, Period7D, now)
	if err != nil {
		t.Fatalf("performance: %v", err)
	}
	if !perf.Partial {
		t.Error("performance must emit partial:true on truncated universe")
	}
	if !perf.AsOf.Equal(u.FetchedAt.UTC()) {
		t.Errorf("performance as_of must be universe fetch time: %v vs %v", perf.AsOf, u.FetchedAt)
	}
	// No refill: activity + performance reuse the 1 universe fetch.
	if got := fake.fillsCalls.Load(); got != 1 {
		t.Fatalf("truncated consumers must not refill: want 1 fetch, got %d", got)
	}
}

// LIVE-U-23: over 128 universes the oldest past 2x TTL drop first; fresh stay;
// non-universe keys untouched.
func TestUniverse_EvictKeepsFreshDropOld(t *testing.T) {
	c := NewOnDemandCache(LiveCacheTTL)
	now := time.Now()
	// 130 universes: 65 stale (>2x TTL), 65 fresh.
	for i := 0; i < 130; i++ {
		k := fmt.Sprintf("fills-universe|hyperliquid|0x%040x", i+1)
		c.set(k, &FillsUniverse{FetchedAt: now})
	}
	c.mu.Lock()
	i := 0
	for k, v := range c.m {
		if i < 65 {
			v.fetchedAt = now.Add(-time.Hour)
			v.expiresAt = v.fetchedAt.Add(c.ttl)
			c.m[k] = v
		}
		i++
		if i >= 130 {
			break
		}
	}
	c.mu.Unlock()
	// Pin non-universe keys that must survive.
	c.set("activity-live|hyperliquid|pinned", &LiveActivitySnapshot{})
	c.set("performance-live|hyperliquid|pinned|30D", &LivePerformanceSnapshot{})

	evictFillsUniverseIfNeeded(c)

	c.mu.Lock()
	universeCount := 0
	staleLeft := 0
	for k, v := range c.m {
		if len(k) >= 15 && k[:15] == "fills-universe|" {
			universeCount++
			if now.Sub(v.fetchedAt) > 2*c.ttl {
				staleLeft++
			}
		}
	}
	c.mu.Unlock()
	if universeCount > fillsUniverseMaxEntries {
		t.Fatalf("universes must be bounded at 128, got %d", universeCount)
	}
	if staleLeft != 0 {
		t.Fatalf("stale (>2x TTL) universes must drop first, %d left", staleLeft)
	}
	if _, _, found := c.get("activity-live|hyperliquid|pinned"); !found {
		t.Error("non-universe keys must survive eviction")
	}
	if _, _, found := c.get("performance-live|hyperliquid|pinned|30D"); !found {
		t.Error("non-universe keys must survive eviction")
	}
}

// LIVE-U-24: TTL expiry refetches; hits within TTL cost zero upstream calls.
func TestUniverse_TTLExpiryRefetch(t *testing.T) {
	fake := &countingUniverseFake{
		raw: []hyperliquid.Fill{
			rawFillForUniverse("BTC", "B", "1", "50000", "0", "1", time.Now().UTC().Add(-time.Hour), 1),
		},
	}
	ttl := 60 * time.Millisecond
	s := universeService(fake, ttl)
	venue := "hyperliquid"
	addr := "0xcccccccccccccccccccccccccccccccccccccccc"

	now1 := time.Now().UTC()
	if _, err := getFillsUniverse(context.Background(), s, venue, addr, now1); err != nil {
		t.Fatalf("first: %v", err)
	}
	if _, err := getFillsUniverse(context.Background(), s, venue, addr, now1); err != nil {
		t.Fatalf("second: %v", err)
	}
	if got := fake.fillsCalls.Load(); got != 1 {
		t.Fatalf("TTL hit must cost 0 refetch, got %d", got)
	}
	time.Sleep(90 * time.Millisecond)
	now2 := time.Now().UTC()
	if _, err := getFillsUniverse(context.Background(), s, venue, addr, now2); err != nil {
		t.Fatalf("expired: %v", err)
	}
	if got := fake.fillsCalls.Load(); got != 2 {
		t.Fatalf("TTL expiry must refetch, got %d", got)
	}
}
