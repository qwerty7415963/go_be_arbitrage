//go:build integration

package trader

import (
	"context"
	"testing"
	"time"
)

// OBS-I-01: stale-wallet count (old as_of or error status) for alerting.
func TestRepo_StaleCount(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	t.Cleanup(pool.Close)
	repo := NewRepository(pool)
	venueID := testVenue(t, pool)
	now := time.Now().UTC()

	mk := func(offset time.Duration, status DataStatus) string {
		addr := testAddr()
		if _, _, err := repo.UpsertRegistry(ctx, venueID, addr,
			SourceLeaderboard, nil, nil); err != nil {
			t.Fatalf("registry: %v", err)
		}
		if err := repo.UpsertPeriodMetrics(ctx, &PeriodMetrics{
			VenueID: venueID, WalletAddress: addr, Period: Period30D,
			AsOf: now.Add(offset), DataStatus: status, CalculationVersion: 1,
		}); err != nil {
			t.Fatalf("period: %v", err)
		}
		return addr
	}
	fresh := mk(-time.Hour, DataReady)
	old := mk(-48*time.Hour, DataReady)
	bad := mk(-time.Hour, DataError)
	t.Cleanup(func() {
		for _, a := range []string{fresh, old, bad} {
			_, _ = pool.Exec(context.Background(),
				`DELETE FROM trader_registry WHERE venue_id = $1 AND wallet_address = $2`, venueID, a)
		}
	})

	n, err := repo.StaleCount(ctx, venueID, Period30D, now.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("stale count: %v", err)
	}
	if n != 2 { // old vintage + error status; fresh excluded
		t.Errorf("want 2 stale, got %d", n)
	}
}
