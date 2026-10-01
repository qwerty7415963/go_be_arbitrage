package trader

import (
	"context"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// Observability for the v1.1 scanner (spec §15, V1 subset): in-memory run
// counters + structured per-run log lines. Alert thresholds (no alert infra
// in this repo — wire these to your monitor):
//   - discovery consecutive errors >= 3
//   - sync failed/(done+failed) sustained high over consecutive runs
//   - stale-wallet count (StaleCount) growing across runs
// A Prometheus /metrics exposition is future work; Stats() snapshots are the
// integration point.

// DiscoveryStats is a point-in-time snapshot of discovery health.
type DiscoveryStats struct {
	Runs              int64
	LastRunAt         time.Time
	LastDuration      time.Duration
	Fetched           int64
	Inserted          int64
	Updated           int64
	Skipped           int64
	ConsecutiveErrors int64
	LastError         string
}

type discoveryCounters struct {
	runs     atomic.Int64
	fetched  atomic.Int64
	inserted atomic.Int64
	updated  atomic.Int64
	skipped  atomic.Int64
	errors   atomic.Int64
	lastRun  atomic.Int64 // unix nano
	lastDur  atomic.Int64 // ns
	lastErr  atomic.Value // string
}

func (s *DiscoveryService) recordRun(start time.Time, res *DiscoveryResult, err error) {
	s.counters.runs.Add(1)
	s.counters.lastRun.Store(time.Now().UTC().UnixNano())
	s.counters.lastDur.Store(int64(time.Since(start)))
	if err != nil {
		s.counters.errors.Add(1)
		s.counters.lastErr.Store(err.Error())
		s.logf("discovery run failed consecutive_errors=%d error=%v",
			s.counters.errors.Load(), err)
		return
	}
	s.counters.errors.Store(0)
	s.counters.lastErr.Store("")
	if res != nil {
		s.counters.fetched.Add(int64(res.Fetched))
		s.counters.inserted.Add(int64(res.Inserted))
		s.counters.updated.Add(int64(res.Updated))
		s.counters.skipped.Add(int64(res.Skipped))
	}
}

// Stats snapshots discovery health.
func (s *DiscoveryService) Stats() DiscoveryStats {
	lastErr, _ := s.counters.lastErr.Load().(string)
	return DiscoveryStats{
		Runs:              s.counters.runs.Load(),
		LastRunAt:         time.Unix(0, s.counters.lastRun.Load()).UTC(),
		LastDuration:      time.Duration(s.counters.lastDur.Load()),
		Fetched:           s.counters.fetched.Load(),
		Inserted:          s.counters.inserted.Load(),
		Updated:           s.counters.updated.Load(),
		Skipped:           s.counters.skipped.Load(),
		ConsecutiveErrors: s.counters.errors.Load(),
		LastError:         lastErr,
	}
}

// SyncStats is a point-in-time snapshot of sync health.
type SyncStats struct {
	Runs              int64
	LastRunAt         time.Time
	LastDuration      time.Duration
	WalletsDone       int64
	WalletsFailed     int64
	FetchErrors       int64
	RateLimitErrors   int64
	UpstreamLatencyMs int64 // cumulative fetch latency
	ConsecutiveErrors int64 // consecutive failed wallet passes
	LastError         string
}

type syncCounters struct {
	runs      atomic.Int64
	done      atomic.Int64
	failed    atomic.Int64
	fetchErrs atomic.Int64
	rateLimit atomic.Int64
	latencyMs atomic.Int64
	errors    atomic.Int64
	lastRun   atomic.Int64
	lastDur   atomic.Int64
	lastErr   atomic.Value
}

func isRateLimit(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "429") || strings.Contains(s, "rate limit") ||
		strings.Contains(s, "too many requests")
}

func (s *SyncService) recordWallet(fetchLatency time.Duration, err error) {
	if err != nil {
		s.counters.failed.Add(1)
		s.counters.fetchErrs.Add(1)
		if isRateLimit(err) {
			s.counters.rateLimit.Add(1)
		}
		s.counters.errors.Add(1)
		s.counters.lastErr.Store(err.Error())
		return
	}
	s.counters.done.Add(1)
	s.counters.errors.Store(0)
	s.counters.latencyMs.Add(fetchLatency.Milliseconds())
}

func (s *SyncService) recordRun(start time.Time) {
	s.counters.runs.Add(1)
	s.counters.lastRun.Store(time.Now().UTC().UnixNano())
	s.counters.lastDur.Store(int64(time.Since(start)))
}

// Stats snapshots sync health.
func (s *SyncService) Stats() SyncStats {
	lastErr, _ := s.counters.lastErr.Load().(string)
	return SyncStats{
		Runs:              s.counters.runs.Load(),
		LastRunAt:         time.Unix(0, s.counters.lastRun.Load()).UTC(),
		LastDuration:      time.Duration(s.counters.lastDur.Load()),
		WalletsDone:       s.counters.done.Load(),
		WalletsFailed:     s.counters.failed.Load(),
		FetchErrors:       s.counters.fetchErrs.Load(),
		RateLimitErrors:   s.counters.rateLimit.Load(),
		UpstreamLatencyMs: s.counters.latencyMs.Load(),
		ConsecutiveErrors: s.counters.errors.Load(),
		LastError:         lastErr,
	}
}

// StaleCount counts period rows older than cutoff or in error (spec §15:
// stale-wallet count for alerting).
func (r *Repository) StaleCount(ctx context.Context, venueID uuid.UUID, period string, olderThan time.Time) (int64, error) {
	var n int64
	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM trader_period_metrics
		WHERE venue_id = $1 AND period = $2
		  AND (as_of < $3 OR data_status = 'error')`,
		venueID, period, olderThan.UTC()).Scan(&n)
	return n, err
}
