//go:build integration

package trader

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

// B1: 404 unknown wallet / 400 bad address / 404 unknown venue.
func TestRequestSync_Validation(t *testing.T) {
	_, svc, addr := syncFixture(t, &fakeFills{})
	ctx := context.Background()
	_ = addr

	if _, err := svc.RequestSync(ctx, testVenueCode, "not-an-address"); err == nil {
		t.Error("bad address must fail")
	} else if appErr := domain.GetAppError(err); appErr.Code != domain.ErrCodeValidation {
		t.Errorf("bad address code: %v", appErr.Code)
	}

	unknown := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if _, err := svc.RequestSync(ctx, testVenueCode, unknown); err == nil {
		t.Error("unknown wallet must 404")
	} else if appErr := domain.GetAppError(err); appErr.Code != domain.ErrCodeNotFound {
		t.Errorf("unknown wallet code: %v", appErr.Code)
	}

	known := svcKnownAddr(t, svc)
	if _, err := svc.RequestSync(ctx, "unknown-venue-xyz", known); err == nil {
		t.Error("unknown venue must 404")
	} else if appErr := domain.GetAppError(err); appErr.Code != domain.ErrCodeNotFound {
		t.Errorf("unknown venue code: %v", appErr.Code)
	}
}

func svcKnownAddr(t *testing.T, svc *SyncService) string {
	t.Helper()
	// syncFixture seeds exactly one registry row; find it via the queue.
	ctx := context.Background()
	items, err := svc.repo.ListSyncQueue(ctx, svc.venueID)
	if err != nil || len(items) == 0 {
		t.Fatalf("queue: %v %d", err, len(items))
	}
	return items[0].Address
}

// B1: queued → drain → recent (debounce, no new pass).
func TestRequestSync_QueuedThenRecent(t *testing.T) {
	repo, svc, addr := syncFixture(t, &fakeFills{})
	ctx := context.Background()
	_ = repo

	st, err := svc.RequestSync(ctx, testVenueCode, addr)
	if err != nil || st != SyncPriorityQueued {
		t.Fatalf("first: %s err=%v", st, err)
	}
	if svc.PriorityQueueLen() != 1 {
		t.Fatalf("queue len: %d", svc.PriorityQueueLen())
	}
	// Drain synchronously (same work as StartPriority's runPriorityOne).
	select {
	case queued := <-svc.prioCh:
		if queued != addr {
			t.Fatalf("queued addr: %s", queued)
		}
		svc.runPriorityOne(context.Background(), queued)
	default:
		t.Fatal("priority channel must hold the enqueued wallet")
	}
	st2, err := svc.RequestSync(ctx, testVenueCode, addr)
	if err != nil || st2 != SyncPriorityRecent {
		t.Fatalf("after fresh sync must be recent: %s err=%v", st2, err)
	}
	if svc.PriorityQueueLen() != 0 {
		t.Errorf("debounced trigger must not enqueue: len=%d", svc.PriorityQueueLen())
	}
}

// B1: failed passes never debounce — error rows stay retryable via both the
// memory path (no prioLastDone entry) and the DB path (LastFillsSyncAt
// untouched), so a retrigger after a failed drain reports `queued`, never
// `recent`.
func TestRequestSync_FailedPassNoDebounce(t *testing.T) {
	_, svc, addr := syncFixture(t, &fakeFills{err: errors.New("boom")})
	ctx := context.Background()

	st, err := svc.RequestSync(ctx, testVenueCode, addr)
	if err != nil || st != SyncPriorityQueued {
		t.Fatalf("first: %s err=%v", st, err)
	}
	select {
	case queued := <-svc.prioCh:
		svc.runPriorityOne(context.Background(), queued)
	default:
		t.Fatal("priority channel must hold the enqueued wallet")
	}
	st2, err := svc.RequestSync(ctx, testVenueCode, addr)
	if err != nil || st2 != SyncPriorityQueued {
		t.Fatalf("after failed pass must re-queue (never recent): %s err=%v", st2, err)
	}
	if svc.PriorityQueueLen() != 1 {
		t.Errorf("failed pass must enqueue again: len=%d", svc.PriorityQueueLen())
	}
}

// B1: concurrent double-trigger ⇒ one pass (one queued, one in_flight).
func TestRequestSync_ConcurrentDoubleTrigger(t *testing.T) {
	fetch := &fakeFills{}
	_, svc, addr := syncFixture(t, fetch)
	ctx := context.Background()

	var wg sync.WaitGroup
	results := make([]string, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			st, err := svc.RequestSync(ctx, testVenueCode, addr)
			if err != nil {
				t.Errorf("request: %v", err)
				return
			}
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
			t.Fatalf("unexpected status %q (%v)", st, results)
		}
	}
	if queued != 1 || inflight != 1 {
		t.Fatalf("one pass only: queued=%d in_flight=%d (%v)", queued, inflight, results)
	}
	if svc.PriorityQueueLen() != 1 {
		t.Fatalf("exactly one enqueue: len=%d", svc.PriorityQueueLen())
	}
	// Drain the single entry: exactly one wallet pass runs.
	select {
	case queued := <-svc.prioCh:
		svc.runPriorityOne(context.Background(), queued)
	default:
		t.Fatal("expected one queued entry")
	}
	if fetch.calls != 1 {
		t.Errorf("one pass must run: fetch calls=%d", fetch.calls)
	}
}

// B1: full backlog never hangs the trigger — 429 COMMON-905 with the inflight
// mark released, so freeing one slot lets the next trigger queue again.
func TestRequestSync_QueueFullBusy(t *testing.T) {
	_, svc, addr := syncFixture(t, &fakeFills{})
	ctx := context.Background()
	for i := 0; i < 1024; i++ {
		svc.prioCh <- fmt.Sprintf("0x%040x", i)
	}
	st, err := svc.RequestSync(ctx, testVenueCode, addr)
	if err == nil {
		t.Fatalf("full queue must error, got %s", st)
	}
	var appErr *domain.AppError
	if !errors.As(err, &appErr) || appErr.Code != domain.ErrCodeRateLimited {
		t.Fatalf("want COMMON-905, got %v", err)
	}
	svc.prioMu.Lock()
	marked := svc.prioInflight[addr]
	svc.prioMu.Unlock()
	if marked {
		t.Fatal("busy trigger must release the inflight mark")
	}
	<-svc.prioCh // free one slot
	st2, err := svc.RequestSync(ctx, testVenueCode, addr)
	if err != nil || st2 != SyncPriorityQueued {
		t.Fatalf("after freeing a slot must queue: %s err=%v", st2, err)
	}
}

// B1: priority drain end-to-end — unsynced wallet becomes ready.
func TestPriorityDrain_UnsyncedToReady(t *testing.T) {
	now := time.Now().UTC()
	repo, svc, addr := syncFixture(t, &fakeFills{fills: dayFills(now)})
	ctx := context.Background()
	venueID, _ := repo.VenueIDByCode(ctx, testVenueCode)

	st, err := svc.RequestSync(ctx, testVenueCode, addr)
	if err != nil || st != SyncPriorityQueued {
		t.Fatalf("trigger: %s err=%v", st, err)
	}
	ctxDrain, cancel := context.WithCancel(context.Background())
	defer cancel()
	go svc.StartPriority(ctxDrain)
	deadline := time.Now().Add(10 * time.Second)
	for {
		state, _ := repo.GetSyncState(ctx, venueID, addr)
		if state != nil && state.BackfillCompletedAt != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("priority drain never completed backfill")
		}
		time.Sleep(50 * time.Millisecond)
	}
	// Debounce now applies (DB LastFillsSyncAt fresh). BackfillCompletedAt is
	// set mid-pass while the inflight mark clears only at pass end, so poll
	// until `recent`, tolerating transient `in_flight` (a pass genuinely
	// still running — honest, not a bug).
	deadline2 := time.Now().Add(10 * time.Second)
	for {
		st2, err := svc.RequestSync(ctx, testVenueCode, addr)
		if err == nil && st2 == SyncPriorityRecent {
			break
		}
		if err != nil || st2 != SyncPriorityInFlight {
			t.Fatalf("after drain must settle at recent (via in_flight): %s err=%v", st2, err)
		}
		if time.Now().After(deadline2) {
			t.Fatalf("after drain never recent, stuck in_flight")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// B2: Activity data_status from sync state (integration via Service.Activity).
func TestActivity_DataStatus_FromSyncState(t *testing.T) {
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
	svc := NewService(repo, nil, []byte("test-secret-32-bytes-for-activity"))
	now := time.Now().UTC()

	// Fresh (backfill never completed) → syncing, rows [] never null.
	page, err := svc.Activity(ctx, testVenueCode, addr, ActivityQuery{})
	if err != nil {
		t.Fatalf("activity fresh: %v", err)
	}
	if page.DataStatus != DataSyncing {
		t.Errorf("fresh must be syncing: %s", page.DataStatus)
	}
	if page.Rows == nil {
		t.Error("rows must be [] never null")
	}

	setState := func(status string, backfill, fills *time.Time) {
		t.Helper()
		if _, err := pool.Exec(ctx, `UPDATE trader_sync_state SET sync_status=$3,
			backfill_completed_at=$4, last_fills_sync_at=$5, updated_at=NOW()
			WHERE venue_id=$1 AND wallet_address=$2`, venueID, addr, status, backfill, fills); err != nil {
			t.Fatalf("set state: %v", err)
		}
	}
	completed := now.Add(-time.Hour)
	recent := now.Add(-time.Hour)
	old := now.Add(-25 * time.Hour)

	setState("error", &completed, &recent)
	if page, _ := svc.Activity(ctx, testVenueCode, addr, ActivityQuery{}); page.DataStatus != DataError {
		t.Errorf("error pass must be error: %s", page.DataStatus)
	}
	setState("ready", &completed, &old)
	if page, _ := svc.Activity(ctx, testVenueCode, addr, ActivityQuery{}); page.DataStatus != DataStale {
		t.Errorf("fills >24h must be stale: %s", page.DataStatus)
	}
	setState("ready", &completed, &recent)
	if page, _ := svc.Activity(ctx, testVenueCode, addr, ActivityQuery{}); page.DataStatus != DataReady {
		t.Errorf("fresh fills must be ready: %s", page.DataStatus)
	}
	// Backfill incomplete never stale even with ancient fills.
	setState("ready", nil, &old)
	if page, _ := svc.Activity(ctx, testVenueCode, addr, ActivityQuery{}); page.DataStatus != DataSyncing {
		t.Errorf("backfill incomplete must be syncing never stale: %s", page.DataStatus)
	}
}

// B3: watched wallet with NO sync row gets a snapshot on the next tick
// (never skip).
func TestSyncPositionsWatched_NilState(t *testing.T) {
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
	// No sync row yet: GetSyncState must be nil.
	if state, err := repo.GetSyncState(ctx, venueID, addr); err != nil || state != nil {
		t.Fatalf("precondition: no sync row: %+v err=%v", state, err)
	}
	hub := NewActivityHub()
	hub.Subscribe(addr)
	svc := NewSyncService(repo, &fakeFills{}, venueID, DefaultSyncOptions()).
		WithPositions(&fakePositions{snap: posSnap("BTC")}).
		WithActivityHub(hub)

	if done := svc.SyncPositionsWatched(ctx, time.Now().UTC()); done != 1 {
		t.Fatalf("nil-state watched wallet must sync: done=%d", done)
	}
	got, err := repo.GetPositions(ctx, venueID, addr)
	if err != nil || len(got) != 1 || got[0].Coin != "BTC" {
		t.Fatalf("snapshot: %+v err=%v", got, err)
	}
	state, _ := repo.GetSyncState(ctx, venueID, addr)
	if state == nil || state.LastPositionsSyncAt == nil {
		t.Errorf("sync row + timestamp must exist: %+v", state)
	}
}
