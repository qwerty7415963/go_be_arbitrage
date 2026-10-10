package trader

import (
	"context"
	"sort"
	"strings"
	"time"
)

// fillsUniverseMaxEntries bounds the shared fills-universe cache: at most
// 128 wallet universes are retained; beyond that the oldest entries (past
// 2x TTL first, then oldest regardless) are dropped.
const fillsUniverseMaxEntries = 128

// FillsUniverse is the single 30d userFillsByTime universe per (venue, wallet):
// normalized fills + venue-truncation flag + fetch time. All live consumers
// (Detail via fetchLivePerformance, Activity, Performance 1D/7D/30D) slice
// this universe in-memory instead of issuing their own window fetch.
// Cached 12s (LiveCacheTTL) + single-flight via s.cache.
type FillsUniverse struct {
	Fills     []Fill
	Truncated bool
	FetchedAt time.Time
}

// fillsUniverseKey returns the shared cache key for one wallet universe.
func fillsUniverseKey(venue, addr string) string {
	return "fills-universe|" + strings.ToLower(strings.TrimSpace(venue)) + "|" + addr
}

// getFillsUniverse returns the cached 30d fills universe for (venue, addr),
// fetching exactly once per TTL window (single-flight). The fetch covers
// [now-30d, now] via FetchFillsWindow + mapHLFillsToTrader; FetchedAt is the
// requesting now (UTC) and becomes the consumers' as_of. Truncation (venue
// 10k cap) is preserved: consumers slice + emit partial:true without any
// refill fetch. Errors are not cached (OnDemandCache semantics).
func getFillsUniverse(ctx context.Context, s *Service, venue, addr string, now time.Time) (*FillsUniverse, error) {
	if s == nil || s.onDemand == nil {
		return nil, errUpstreamUnavailable
	}
	if s.cache == nil {
		startMs := now.Add(-LiveActivityWindow).UnixMilli()
		endMs := now.UnixMilli()
		raw, truncated, err := s.onDemand.FetchFillsWindow(ctx, addr, startMs, endMs)
		if err != nil {
			return nil, err
		}
		return &FillsUniverse{Fills: mapHLFillsToTrader(raw), Truncated: truncated, FetchedAt: now.UTC()}, nil
	}
	key := fillsUniverseKey(venue, addr)
	val, _, _, err := s.cache.GetOrFetch(key, func() (any, error) {
		startMs := now.Add(-LiveActivityWindow).UnixMilli()
		endMs := now.UnixMilli()
		raw, truncated, err := s.onDemand.FetchFillsWindow(ctx, addr, startMs, endMs)
		if err != nil {
			return nil, err
		}
		return &FillsUniverse{Fills: mapHLFillsToTrader(raw), Truncated: truncated, FetchedAt: now.UTC()}, nil
	})
	if err != nil {
		return nil, err
	}
	u, ok := val.(*FillsUniverse)
	if !ok || u == nil {
		return nil, errUpstreamUnavailable
	}
	evictFillsUniverseIfNeeded(s.cache)
	return u, nil
}

// sliceUniverse cuts universe rows to [now-window, now] preserving input
// order (universe is oldest-first). Boundary-inclusive on both ends.
func sliceUniverse(fills []Fill, now time.Time, window time.Duration) []Fill {
	if len(fills) == 0 || window <= 0 {
		return []Fill{}
	}
	cutoff := now.Add(-window)
	out := make([]Fill, 0, len(fills))
	for _, f := range fills {
		if f.FilledAt.Before(cutoff) {
			continue
		}
		if f.FilledAt.After(now) {
			continue
		}
		out = append(out, f)
	}
	return out
}

// evictFillsUniverseIfNeeded drops oldest fills-universe entries when more
// than fillsUniverseMaxEntries wallet universes are cached. When over the cap,
// ALL entries older than 2x TTL drop first (oldest-first by cache fetchedAt);
// if still over the cap, the oldest remaining go regardless (memory bound).
// Non-universe keys are never touched.
func evictFillsUniverseIfNeeded(c *OnDemandCache) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	type entry struct {
		key       string
		fetchedAt time.Time
	}
	var universes []entry
	for k, v := range c.m {
		if strings.HasPrefix(k, "fills-universe|") {
			universes = append(universes, entry{key: k, fetchedAt: v.fetchedAt})
		}
	}
	if len(universes) <= fillsUniverseMaxEntries {
		return
	}
	sort.Slice(universes, func(i, j int) bool {
		return universes[i].fetchedAt.Before(universes[j].fetchedAt)
	})
	ttl := c.ttl
	if ttl <= 0 {
		ttl = LiveCacheTTL
	}
	now := time.Now()
	for _, e := range universes {
		if it, ok := c.m[e.key]; ok && now.Sub(it.fetchedAt) > 2*ttl {
			delete(c.m, e.key)
		}
	}
	for _, e := range universes {
		if len(countUniverseKeysLocked(c)) <= fillsUniverseMaxEntries {
			break
		}
		if _, ok := c.m[e.key]; ok {
			delete(c.m, e.key)
		}
	}
}

func countUniverseKeysLocked(c *OnDemandCache) []string {
	var out []string
	for k := range c.m {
		if strings.HasPrefix(k, "fills-universe|") {
			out = append(out, k)
		}
	}
	return out
}
