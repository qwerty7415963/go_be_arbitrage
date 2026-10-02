//go:build integration

package trader

import (
	"context"
	"testing"
	"time"
)

// WS-I-01/02: flush inserts ws_trade rows with max last_trade_at;
// a second identical flush is idempotent.
func TestWSHarvest_Flush(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	t.Cleanup(pool.Close)
	repo := NewRepository(pool)
	venueID := testVenue(t, pool)
	svc := NewWSHarvestService(repo, venueID, 500, time.Second)

	a1, a2 := testAddr(), testAddr()
	cleanupAddrs(t, pool, venueID, a1, a2)
	base := time.Now().UTC().Truncate(time.Second)
	svc.pending[a1] = base
	svc.pending[a2] = base.Add(-time.Hour)
	// Duplicate sightings collapse to max time before flush.
	svc.pending[a1] = base.Add(time.Minute)
	if err := svc.flush(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}
	for addr, wantSource := range map[string]DiscoverySource{a1: SourceWSTrade, a2: SourceWSTrade} {
		got, err := repo.GetRegistry(ctx, venueID, addr)
		if err != nil {
			t.Fatalf("get %s: %v", addr, err)
		}
		if got.DiscoverySource != wantSource {
			t.Errorf("%s source: %s", addr, got.DiscoverySource)
		}
	}
	got, _ := repo.GetRegistry(ctx, venueID, a1)
	if !got.LastTradeAt.Equal(base.Add(time.Minute)) {
		t.Errorf("last_trade_at max: %v", got.LastTradeAt)
	}
	if stats := svc.Stats(); stats.Flushes != 1 || stats.Pending != 0 {
		t.Errorf("stats: %+v", stats)
	}

	// Idempotent rerun.
	svc.pending[a1] = base
	if err := svc.flush(ctx); err != nil {
		t.Fatalf("reflush: %v", err)
	}
	got, _ = repo.GetRegistry(ctx, venueID, a1)
	if !got.LastTradeAt.Equal(base.Add(time.Minute)) {
		t.Errorf("reflush must keep max time: %v", got.LastTradeAt)
	}
}

// WS-I-03: leaderboard-seeded wallet seen on WS merges to both.
func TestWSHarvest_Merge(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	t.Cleanup(pool.Close)
	repo := NewRepository(pool)
	venueID := testVenue(t, pool)
	svc := NewWSHarvestService(repo, venueID, 500, time.Second)

	addr := testAddr()
	cleanupAddrs(t, pool, venueID, addr)
	name := "LB Whale"
	first, _, err := repo.UpsertRegistry(ctx, venueID, addr, SourceLeaderboard, &name, nil)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	svc.pending[addr] = time.Now().UTC()
	if err := svc.flush(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}
	got, _ := repo.GetRegistry(ctx, venueID, addr)
	if got.DiscoverySource != SourceBoth {
		t.Errorf("merge: want both, got %s", got.DiscoverySource)
	}
	if !got.FirstSeenAt.Equal(first.FirstSeenAt) {
		t.Error("first_seen_at must not move")
	}
	if got.DisplayName == nil || *got.DisplayName != name {
		t.Error("display_name must survive the merge")
	}
}
