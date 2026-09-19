package fundingarbitrage

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// ─── Cache tests ──────────────────────────────────────────────

func TestCache_Get_Miss(t *testing.T) {
	c := NewCache(time.Minute)
	_, ok := c.Get(uuid.New(), uuid.New())
	if ok {
		t.Error("expected cache miss")
	}
}

func TestCache_SetAndGet_Hit(t *testing.T) {
	c := NewCache(time.Minute)
	idA := uuid.New()
	idB := uuid.New()

	tokens := []ArbitrageToken{{Symbol: "BTC"}}
	c.Set(idA, idB, tokens)

	got, ok := c.Get(idA, idB)
	if !ok {
		t.Fatal("expected cache hit")
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 token, got %d", len(got))
	}
	if got[0].Symbol != "BTC" {
		t.Errorf("expected BTC, got %s", got[0].Symbol)
	}
}

func TestCache_SetAndGet_SymmetricKey(t *testing.T) {
	c := NewCache(time.Minute)
	idA := uuid.New()
	idB := uuid.New()

	tokens := []ArbitrageToken{{Symbol: "ETH"}}
	c.Set(idA, idB, tokens)

	got, ok := c.Get(idB, idA)
	if !ok {
		t.Fatal("expected cache hit with reversed keys")
	}
	if got[0].Symbol != "ETH" {
		t.Errorf("expected ETH, got %s", got[0].Symbol)
	}
}

func TestCache_Invalidate(t *testing.T) {
	c := NewCache(time.Minute)
	idA := uuid.New()
	idB := uuid.New()
	idC := uuid.New()

	c.Set(idA, idB, []ArbitrageToken{{Symbol: "BTC"}})
	c.Set(idA, idC, []ArbitrageToken{{Symbol: "ETH"}})

	c.Invalidate(idA)

	_, ok := c.Get(idA, idB)
	if ok {
		t.Error("expected cache miss after invalidation")
	}
	_, ok = c.Get(idA, idC)
	if ok {
		t.Error("expected cache miss after invalidation")
	}
}

func TestCache_GetOrFetch_CacheHit(t *testing.T) {
	c := NewCache(time.Minute)
	idA := uuid.New()
	idB := uuid.New()

	c.Set(idA, idB, []ArbitrageToken{{Symbol: "SOL"}})

	fetchCalled := false
	got, source, err := c.GetOrFetch(idA, idB, func() ([]ArbitrageToken, error) {
		fetchCalled = true
		return nil, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fetchCalled {
		t.Error("fetchFn should not be called on cache hit")
	}
	if source != "fresh" {
		t.Errorf("expected source fresh, got %s", source)
	}
	if len(got) != 1 || got[0].Symbol != "SOL" {
		t.Errorf("expected SOL token, got %v", got)
	}
}

func TestCache_GetOrFetch_CacheMiss(t *testing.T) {
	c := NewCache(time.Minute)
	idA := uuid.New()
	idB := uuid.New()

	expected := []ArbitrageToken{{Symbol: "DOGE"}}
	got, source, err := c.GetOrFetch(idA, idB, func() ([]ArbitrageToken, error) {
		return expected, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if source != "miss" {
		t.Errorf("expected source miss, got %s", source)
	}
	if len(got) != 1 || got[0].Symbol != "DOGE" {
		t.Errorf("expected DOGE, got %v", got)
	}

	// Should now be cached
	_, ok := c.Get(idA, idB)
	if !ok {
		t.Error("expected value to be cached after fetch")
	}
}

func TestCache_GetCacheStatus(t *testing.T) {
	c := NewCache(time.Minute)
	idA := uuid.New()
	idB := uuid.New()

	if c.GetCacheStatus(idA, idB) != "miss" {
		t.Error("expected miss for empty cache")
	}

	c.Set(idA, idB, []ArbitrageToken{{Symbol: "X"}})
	if c.GetCacheStatus(idA, idB) != "fresh" {
		t.Error("expected fresh after set")
	}
}

// ─── SortTokens tests ────────────────────────────────────────

func ptrFloat64(f float64) *float64 { return &f }

func TestSortTokens_ByAPR1h(t *testing.T) {
	tokens := []ArbitrageToken{
		{Symbol: "A", APR1hPercent: ptrFloat64(5.0)},
		{Symbol: "B", APR1hPercent: ptrFloat64(15.0)},
		{Symbol: "C", APR1hPercent: ptrFloat64(10.0)},
	}
	SortTokens(tokens, "apr_1h_desc")
	if tokens[0].Symbol != "B" || tokens[1].Symbol != "C" || tokens[2].Symbol != "A" {
		t.Errorf("unexpected sort order: %v", tokens)
	}
}

func TestSortTokens_BySpread(t *testing.T) {
	tokens := []ArbitrageToken{
		{Symbol: "X", PriceSpreadPercent: ptrFloat64(1.0)},
		{Symbol: "Y", PriceSpreadPercent: ptrFloat64(5.0)},
		{Symbol: "Z", PriceSpreadPercent: ptrFloat64(3.0)},
	}
	SortTokens(tokens, "spread_desc")
	if tokens[0].Symbol != "Y" || tokens[1].Symbol != "Z" || tokens[2].Symbol != "X" {
		t.Errorf("unexpected sort order: %v", tokens)
	}
}

func TestSortTokens_NilValues(t *testing.T) {
	tokens := []ArbitrageToken{
		{Symbol: "A", APR1hPercent: nil},
		{Symbol: "B", APR1hPercent: ptrFloat64(10.0)},
		{Symbol: "C", APR1hPercent: nil},
	}
	SortTokens(tokens, "apr_1h_desc")
	if tokens[0].Symbol != "B" {
		t.Errorf("expected B first, got %s", tokens[0].Symbol)
	}
}
