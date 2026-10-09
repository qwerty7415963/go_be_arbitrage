package trader

import (
	"context"
	"strings"
	"time"

	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

// SyncPriorityDebounceWindow is the SYNC-FIX v1.1 B1 debounce: a wallet whose
// sync completed within this window reports `recent` instead of enqueueing a
// new pass. Const (testable); per-service override via WithPriorityDebounce
// exists only for fast unit tests.
const SyncPriorityDebounceWindow = 10 * time.Minute

// Sync priority statuses for POST /api/v1/traders/{wallet}/sync (202 data).
const (
	SyncPriorityQueued   = "queued"
	SyncPriorityInFlight = "in_flight"
	SyncPriorityRecent   = "recent"
)

// SyncResponse is the POST /traders/{wallet}/sync 202 payload.
type SyncResponse struct {
	Status string `json:"status"`
}

// ensurePriorityInit lazily initializes the priority-lane state so zero-value
// SyncService values remain usable in unit tests.
func (s *SyncService) ensurePriorityInit() {
	if s.prioInflight == nil {
		s.prioInflight = map[string]bool{}
	}
	if s.prioLastDone == nil {
		s.prioLastDone = map[string]time.Time{}
	}
	if s.prioCh == nil {
		s.prioCh = make(chan string, 1024)
	}
	if s.prioDebounce <= 0 {
		s.prioDebounce = SyncPriorityDebounceWindow
	}
	if s.prioNow == nil {
		s.prioNow = time.Now
	}
}

// WithPriorityDebounce overrides the debounce window (tests only; production
// always uses SyncPriorityDebounceWindow).
func (s *SyncService) WithPriorityDebounce(d time.Duration) *SyncService {
	s.ensurePriorityInit()
	s.prioDebounce = d
	return s
}

// WithPriorityNow overrides the clock for debounce decisions (tests only).
func (s *SyncService) WithPriorityNow(fn func() time.Time) *SyncService {
	s.ensurePriorityInit()
	s.prioNow = fn
	return s
}

// PriorityQueueLen reports the pending priority depth (tests + observability).
func (s *SyncService) PriorityQueueLen() int {
	if s.prioCh == nil {
		return 0
	}
	return len(s.prioCh)
}

// priorityCheckAndMark is the in-memory singleflight + debounce core,
// unit-testable without DB. Returns (status, enqueued): `recent` and
// `in_flight` never enqueue; `queued` marks inflight and the caller must
// enqueue the address on the priority channel.
func (s *SyncService) priorityCheckAndMark(addr string, now time.Time) (string, bool) {
	s.ensurePriorityInit()
	s.prioMu.Lock()
	defer s.prioMu.Unlock()
	if last, ok := s.prioLastDone[addr]; ok && now.Sub(last) < s.prioDebounce {
		return SyncPriorityRecent, false
	}
	if s.prioInflight[addr] {
		return SyncPriorityInFlight, false
	}
	s.prioInflight[addr] = true
	return SyncPriorityQueued, true
}

// RequestSync enqueues ONE priority full SyncWallet pass for the wallet
// (SYNC-FIX v1.1 B1). Validation mirrors the Detail path: bad address → 400
// (COMMON-902/INVALID_FILTER), unknown wallet/venue → 404. Success returns
// one of `queued` (enqueued), `in_flight` (already running — singleflight
// no-op) or `recent` (completed within the 10-min debounce — no-op). A full
// backlog returns 429 COMMON-905 (never blocks the caller).
//
// The priority lane has a dedicated drain (StartPriority, wired in app.Run)
// sharing the venue pacer (same HL client) and the worker cap (sequential
// drain <= opts.Workers); the 6h SyncAll cadence is unchanged.
func (s *SyncService) RequestSync(ctx context.Context, venueCode, rawAddr string) (string, error) {
	addr, err := NormalizeAddress(rawAddr)
	if err != nil {
		return "", err
	}
	venue := strings.ToLower(strings.TrimSpace(venueCode))
	if venue == "" {
		venue = DefaultVenue
	}
	venueID, err := s.repo.VenueIDByCode(ctx, venue)
	if err != nil {
		return "", domain.NewError(domain.ErrCodeNotFound, "unknown venue")
	}
	if venueID != s.venueID {
		return "", domain.NewError(domain.ErrCodeNotFound, "unknown venue")
	}
	if _, err := s.repo.GetRegistry(ctx, venueID, addr); err != nil {
		return "", err
	}
	s.ensurePriorityInit()
	now := s.prioNow().UTC()

	// Fast path: in-memory debounce + singleflight.
	s.prioMu.Lock()
	if last, ok := s.prioLastDone[addr]; ok && now.Sub(last) < s.prioDebounce {
		s.prioMu.Unlock()
		return SyncPriorityRecent, nil
	}
	if s.prioInflight[addr] {
		s.prioMu.Unlock()
		return SyncPriorityInFlight, nil
	}
	s.prioMu.Unlock()

	// Durable debounce: a SyncAll or priority pass that completed recently
	// (DB LastFillsSyncAt within the window) also reports `recent`, so a
	// fresh wallet is not re-synced after every view. Errors never block
	// retry: only successful fills timestamps count.
	if state, err := s.repo.GetSyncState(ctx, venueID, addr); err == nil && state != nil &&
		state.LastFillsSyncAt != nil && now.Sub(*state.LastFillsSyncAt) < s.prioDebounce {
		return SyncPriorityRecent, nil
	}

	// Re-check singleflight under lock (a concurrent trigger may have
	// enqueued between the fast path and the DB read) and mark.
	s.prioMu.Lock()
	if s.prioInflight[addr] {
		s.prioMu.Unlock()
		return SyncPriorityInFlight, nil
	}
	s.prioInflight[addr] = true
	s.prioMu.Unlock()

	// Buffered channel (1024): never block the POST handler. On a full
	// backlog, release the mark (a later trigger may retry) and report busy
	// (429) instead of hanging indefinitely; the inflight mark guarantees a
	// concurrent second trigger still reports `in_flight`, never a duplicate.
	select {
	case s.prioCh <- addr:
		return SyncPriorityQueued, nil
	default:
		s.prioMu.Lock()
		delete(s.prioInflight, addr)
		s.prioMu.Unlock()
		return "", domain.NewError(domain.ErrCodeRateLimited, "sync queue full, retry later")
	}
}

// runPriorityOne executes one priority pass, then clears singleflight and —
// on success only (errors stay retryable) — records the debounce timestamp.
// ctx (from StartPriority) aborts an in-flight pass on shutdown; success
// entries older than 2× debounce are evicted past a size cap so the map
// cannot grow with distinct wallets over process lifetime.
func (s *SyncService) runPriorityOne(ctx context.Context, addr string) {
	err := s.SyncWallet(ctx, addr, time.Now().UTC())
	s.prioMu.Lock()
	defer s.prioMu.Unlock()
	delete(s.prioInflight, addr)
	if err == nil {
		s.prioLastDone[addr] = s.prioNow().UTC()
		s.evictPriorityDone()
	}
}

// evictPriorityDone drops debounce entries older than 2× the window once the
// map grows past the cap (caller holds prioMu). Prevents unbounded growth
// with distinct wallets over process lifetime.
func (s *SyncService) evictPriorityDone() {
	if len(s.prioLastDone) <= 2048 {
		return
	}
	cutoff := s.prioNow().UTC().Add(-2 * s.prioDebounce)
	for a, t := range s.prioLastDone {
		if t.Before(cutoff) {
			delete(s.prioLastDone, a)
		}
	}
}

// StartPriority drains the priority lane sequentially until ctx ends
// (SYNC-FIX v1.1 B1: dedicated drain; sequential <= opts.Workers so the
// shared worker cap is never exceeded; the venue pacer is shared via the
// same HL client used by SyncAll).
func (s *SyncService) StartPriority(ctx context.Context) {
	s.ensurePriorityInit()
	for {
		select {
		case <-ctx.Done():
			return
		case addr := <-s.prioCh:
			s.runPriorityOne(ctx, addr)
		}
	}
}
