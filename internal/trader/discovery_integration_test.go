//go:build integration

package trader

import (
	"context"
	"testing"
)

type fakeFetcher struct {
	rows []DiscoveredWallet
	err  error
}

func (f *fakeFetcher) FetchTop(_ context.Context, _ int) ([]DiscoveredWallet, error) {
	return f.rows, f.err
}

// DISC service: discover inserts registry rows + window refs; a rerun updates
// without duplicating and keeps first_seen_at (BE-001/BE-002 service half).
func TestDiscovery_DiscoverAndRerun(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	defer pool.Close()
	repo := NewRepository(pool)
	venueID := testVenue(t, pool)

	a1, a2 := testAddr(), testAddr()
	cleanupAddrs(t, pool, venueID, a1, a2)
	name := "Whale One"
	p1, p2 := 50000.0, 10000.0
	r1, r2 := 0.05, 0.02
	fetch := &fakeFetcher{rows: []DiscoveredWallet{
		{Address: a1, DisplayName: &name,
			Windows: map[string]WindowStats{"month": {PnL: &p1, ROI: &r1}}},
		{Address: a2, Windows: map[string]WindowStats{"month": {PnL: &p2, ROI: &r2}}},
		{Address: "not-an-address", Windows: map[string]WindowStats{}},
	}}
	svc := NewDiscoveryService(repo, fetch, venueID, 10)

	res, err := svc.Discover(ctx)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if res.Fetched != 3 || res.Inserted != 2 || res.Skipped != 1 || res.Updated != 0 {
		t.Errorf("counts: %+v", res)
	}

	first, err := repo.GetRegistry(ctx, venueID, a1)
	if err != nil || first.DisplayName == nil || *first.DisplayName != name {
		t.Errorf("registry: %+v %v", first, err)
	}
	ref, err := repo.GetLeaderboardRef(ctx, venueID, a1, "month")
	if err != nil || ref == nil || ref.ROI == nil || *ref.ROI != r1 {
		t.Errorf("ref: %+v %v", ref, err)
	}

	res2, err := svc.Discover(ctx)
	if err != nil {
		t.Fatalf("rerun: %v", err)
	}
	if res2.Inserted != 0 || res2.Updated != 2 {
		t.Errorf("rerun counts: %+v", res2)
	}
	again, _ := repo.GetRegistry(ctx, venueID, a1)
	if !again.FirstSeenAt.Equal(first.FirstSeenAt) {
		t.Errorf("first_seen_at moved: %v -> %v", first.FirstSeenAt, again.FirstSeenAt)
	}
}
