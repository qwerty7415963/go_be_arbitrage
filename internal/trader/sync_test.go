package trader

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// SYNC-U-01: fan-out processes each item exactly once across workers.
func TestFanOut(t *testing.T) {
	var mu sync.Mutex
	seen := map[string]int{}
	done, failed := fanOut(context.Background(),
		[]string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}, 4,
		func(addr string) error {
			mu.Lock()
			seen[addr]++
			mu.Unlock()
			if addr == "c" || addr == "g" {
				return errors.New("boom")
			}
			return nil
		})
	if done != 8 || failed != 2 {
		t.Errorf("done=%d failed=%d", done, failed)
	}
	for a, n := range seen {
		if n != 1 {
			t.Errorf("%s processed %d times", a, n)
		}
	}
	if len(seen) != 10 {
		t.Errorf("coverage: %d", len(seen))
	}
}

// SYNC-U-03: tier classification.
func TestTierFor(t *testing.T) {
	now := time.Now().UTC()
	recent := now.Add(-24 * time.Hour)
	old := now.Add(-30 * 24 * time.Hour)
	if tierFor(&recent, true, now, 7*24*time.Hour) != tierHot {
		t.Error("recent trade must be hot")
	}
	if tierFor(&old, true, now, 7*24*time.Hour) != tierCold {
		t.Error("old trade must be cold")
	}
	if tierFor(nil, true, now, 7*24*time.Hour) != tierCold {
		t.Error("never-traded must be cold")
	}
	if tierFor(&recent, false, now, 7*24*time.Hour) != tierPending {
		t.Error("never-backfilled must be pending")
	}
}
