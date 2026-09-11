package fundingarbitrage

import (
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/singleflight"
)

const (
	defaultTTL       = 30 * time.Second
	maxStaleDuration = 2 * time.Minute
)

type cachedPair struct {
	Tokens    []ArbitrageToken
	UpdatedAt time.Time
}

type Cache struct {
	mu   sync.RWMutex
	data map[string]*cachedPair
	ttl  time.Duration
	sg   singleflight.Group
}

func NewCache(ttl time.Duration) *Cache {
	if ttl == 0 {
		ttl = defaultTTL
	}
	return &Cache{
		data: make(map[string]*cachedPair),
		ttl:  ttl,
	}
}

// makeKey creates a cache key from two venue IDs (sorted)
func makeKey(venueAID, venueBID uuid.UUID) string {
	a, b := venueAID.String(), venueBID.String()
	if a > b {
		a, b = b, a
	}
	return a + ":" + b
}

// Get retrieves cached tokens for a venue pair
func (c *Cache) Get(venueAID, venueBID uuid.UUID) ([]ArbitrageToken, bool) {
	key := makeKey(venueAID, venueBID)

	c.mu.RLock()
	defer c.mu.RUnlock()

	cached, ok := c.data[key]
	if !ok {
		return nil, false
	}

	// Check if still fresh
	if time.Since(cached.UpdatedAt) <= c.ttl {
		return cached.Tokens, true
	}

	// Check if stale but usable (within maxStaleDuration)
	if time.Since(cached.UpdatedAt) <= maxStaleDuration {
		return cached.Tokens, true
	}

	return nil, false
}

// Set stores tokens for a venue pair
func (c *Cache) Set(venueAID, venueBID uuid.UUID, tokens []ArbitrageToken) {
	key := makeKey(venueAID, venueBID)

	c.mu.Lock()
	defer c.mu.Unlock()

	c.data[key] = &cachedPair{
		Tokens:    tokens,
		UpdatedAt: time.Now(),
	}
}

// Invalidate removes all cached pairs containing the given venue
func (c *Cache) Invalidate(venueID uuid.UUID) {
	c.mu.Lock()
	defer c.mu.Unlock()

	venueStr := venueID.String()
	for key := range c.data {
		// Check if this key contains the venue
		if len(key) > 36 {
			venueA := key[:36]
			venueB := key[37:]
			if venueA == venueStr || venueB == venueStr {
				delete(c.data, key)
			}
		}
	}
}

// GetOrFetch retrieves from cache or uses singleflight to fetch
func (c *Cache) GetOrFetch(
	venueAID, venueBID uuid.UUID,
	fetchFn func() ([]ArbitrageToken, error),
) ([]ArbitrageToken, string, error) {
	// Try cache first
	if tokens, ok := c.Get(venueAID, venueBID); ok {
		return tokens, "fresh", nil
	}

	// Use singleflight to prevent duplicate fetches
	key := makeKey(venueAID, venueBID)
	result, err, _ := c.sg.Do(key, func() (interface{}, error) {
		tokens, err := fetchFn()
		if err != nil {
			return nil, err
		}
		c.Set(venueAID, venueBID, tokens)
		return tokens, nil
	})

	if err != nil {
		return nil, "miss", err
	}

	return result.([]ArbitrageToken), "miss", nil
}

// GetCacheStatus returns the cache status for a venue pair
func (c *Cache) GetCacheStatus(venueAID, venueBID uuid.UUID) string {
	key := makeKey(venueAID, venueBID)

	c.mu.RLock()
	defer c.mu.RUnlock()

	cached, ok := c.data[key]
	if !ok {
		return "miss"
	}

	age := time.Since(cached.UpdatedAt)
	if age <= c.ttl {
		return "fresh"
	}
	if age <= maxStaleDuration {
		return "stale"
	}
	return "miss"
}

// SortTokens sorts arbitrage tokens by the given sort field
func SortTokens(tokens []ArbitrageToken, sortBy string) {
	switch sortBy {
	case "apr_1h_desc":
		sort.Slice(tokens, func(i, j int) bool {
			if tokens[i].APR1hPercent == nil {
				return false
			}
			if tokens[j].APR1hPercent == nil {
				return true
			}
			return *tokens[i].APR1hPercent > *tokens[j].APR1hPercent
		})
	case "apr_4h_desc":
		sort.Slice(tokens, func(i, j int) bool {
			if tokens[i].APR4hPercent == nil {
				return false
			}
			if tokens[j].APR4hPercent == nil {
				return true
			}
			return *tokens[i].APR4hPercent > *tokens[j].APR4hPercent
		})
	case "apy_desc":
		sort.Slice(tokens, func(i, j int) bool {
			if tokens[i].APYPercent == nil {
				return false
			}
			if tokens[j].APYPercent == nil {
				return true
			}
			return *tokens[i].APYPercent > *tokens[j].APYPercent
		})
	default: // apr_24h_desc or default
		sort.Slice(tokens, func(i, j int) bool {
			if tokens[i].APR4hPercent == nil {
				return false
			}
			if tokens[j].APR4hPercent == nil {
				return true
			}
			return *tokens[i].APR4hPercent > *tokens[j].APR4hPercent
		})
	}
}
