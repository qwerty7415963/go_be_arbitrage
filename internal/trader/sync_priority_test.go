package trader

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// SYNC-FIX v1.1 B1: debounce window is a 10-min const (testable).
func TestSyncPriorityDebounceWindow_Value(t *testing.T) {
	if SyncPriorityDebounceWindow != 10*time.Minute {
		t.Errorf("debounce must be 10m, got %v", SyncPriorityDebounceWindow)
	}
}

func newPrioritySvc() *SyncService {
	svc := NewSyncService(&Repository{pool: nil}, nil, uuid.New(), DefaultSyncOptions())
	svc.WithPriorityDebounce(10 * time.Minute)
	return svc
}

// B1 unit: singleflight + debounce core without DB.
func TestPriorityCheckAndMark_QueuedThenInFlight(t *testing.T) {
	svc := newPrioritySvc()
	now := time.Now().UTC()
	addr := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	st, enq := svc.priorityCheckAndMark(addr, now)
	if st != SyncPriorityQueued || !enq {
		t.Fatalf("first must queue: %s enq=%v", st, enq)
	}
	// Simulate enqueue drain pending: inflight stays true until runPriorityOne.
	st2, enq2 := svc.priorityCheckAndMark(addr, now)
	if st2 != SyncPriorityInFlight || enq2 {
		t.Fatalf("concurrent double-trigger must be in_flight: %s enq=%v", st2, enq2)
	}
}

func TestPriorityCheckAndMark_RecentWithinDebounce(t *testing.T) {
	svc := newPrioritySvc()
	now := time.Now().UTC()
	addr := "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	st, _ := svc.priorityCheckAndMark(addr, now)
	if st != SyncPriorityQueued {
		t.Fatalf("first: %s", st)
	}
	// Complete successfully → debounce timestamp recorded.
	svc.prioMu.Lock()
	delete(svc.prioInflight, addr)
	svc.prioLastDone[addr] = now
	svc.prioMu.Unlock()

	st2, enq2 := svc.priorityCheckAndMark(addr, now.Add(time.Minute))
	if st2 != SyncPriorityRecent || enq2 {
		t.Fatalf("within 10m must be recent: %s enq=%v", st2, enq2)
	}
	st3, enq3 := svc.priorityCheckAndMark(addr, now.Add(11*time.Minute))
	if st3 != SyncPriorityQueued || !enq3 {
		t.Fatalf("after window must queue again: %s enq=%v", st3, enq3)
	}
}

func TestPriorityCheckAndMark_ConcurrentDoubleTrigger_OnePass(t *testing.T) {
	svc := newPrioritySvc()
	now := time.Now().UTC()
	addr := "0xcccccccccccccccccccccccccccccccccccccccc"

	var wg sync.WaitGroup
	results := make([]string, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			st, _ := svc.priorityCheckAndMark(addr, now)
			results[idx] = st
		}(i)
	}
	wg.Wait()
	queued, inflight := 0, 0
	for _, st := range results {
		switch st {
		case SyncPriorityQueued:
			queued++
		case SyncPriorityInFlight:
			inflight++
		default:
			t.Fatalf("unexpected status %s", st)
		}
	}
	if queued != 1 || inflight != 1 {
		t.Fatalf("exactly one pass: queued=%d in_flight=%d (%v)", queued, inflight, results)
	}
}

// B2 unit: data_status derivation (SYNC-FIX v1.1 §2).
func TestDeriveActivityStatus_Matrix(t *testing.T) {
	now := time.Now().UTC()
	recent := now.Add(-time.Hour)
	old := now.Add(-25 * time.Hour)
	completed := now.Add(-time.Hour)

	cases := []struct {
		name  string
		state *SyncState
		want  DataStatus
	}{
		{"nil state syncing", nil, DataSyncing},
		{"backfill incomplete syncing", &SyncState{SyncStatus: "ready"}, DataSyncing},
		{"backfill incomplete never stale", &SyncState{SyncStatus: "ready", LastFillsSyncAt: &old}, DataSyncing},
		{"error beats everything", &SyncState{SyncStatus: "error", BackfillCompletedAt: &completed, LastFillsSyncAt: &recent}, DataError},
		{"error with no backfill still error", &SyncState{SyncStatus: "error"}, DataError},
		{"stale fills >24h", &SyncState{SyncStatus: "ready", BackfillCompletedAt: &completed, LastFillsSyncAt: &old}, DataStale},
		{"ready fresh", &SyncState{SyncStatus: "ready", BackfillCompletedAt: &completed, LastFillsSyncAt: &recent}, DataReady},
		{"nil fills syncing", &SyncState{SyncStatus: "ready", BackfillCompletedAt: &completed}, DataSyncing},
	}
	for _, tc := range cases {
		if got := deriveActivityStatus(tc.state, now, 24*time.Hour); got != tc.want {
			t.Errorf("%s: want %s got %s", tc.name, tc.want, got)
		}
	}
}

// Eviction bounds prioLastDone: past the cap, entries older than 2× debounce
// drop while fresh ones stay; under the cap nothing drops.
func TestEvictPriorityDone_BoundsMemory(t *testing.T) {
	svc := &SyncService{}
	svc.ensurePriorityInit()
	old := time.Now().UTC().Add(-time.Hour)
	for i := 0; i < 2050; i++ {
		svc.prioLastDone[fmt.Sprintf("0x%040x", i)] = old
	}
	svc.prioLastDone["0xfresh"] = time.Now().UTC()
	svc.prioMu.Lock()
	svc.evictPriorityDone()
	svc.prioMu.Unlock()
	if len(svc.prioLastDone) != 1 {
		t.Errorf("evict must drop old entries past cap, kept %d", len(svc.prioLastDone))
	}
	if _, ok := svc.prioLastDone["0xfresh"]; !ok {
		t.Error("evict must keep fresh entries")
	}

	small := &SyncService{}
	small.ensurePriorityInit()
	small.prioLastDone["0xa"] = old
	small.evictPriorityDone()
	if len(small.prioLastDone) != 1 {
		t.Error("evict under cap must not drop")
	}
}
