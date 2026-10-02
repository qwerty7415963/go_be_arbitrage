package trader

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/hyperliquid"
)

// WSTradeEvent is one buyer/seller pair harvested from a venue trade stream
// (spec §5: only addresses are kept while processing the event).
type WSTradeEvent struct {
	Coin   string
	Time   time.Time
	Buyer  string
	Seller string
}

// mergeBatch folds events into addr→max-trade-time, normalizing addresses and
// skipping malformed entries (spec §6, WS-U-05/06). Pure function, unit-tested.
func mergeBatch(dst map[string]time.Time, events []WSTradeEvent) (merged, skipped int) {
	for _, e := range events {
		for _, raw := range []string{e.Buyer, e.Seller} {
			addr, err := NormalizeAddress(raw)
			if err != nil {
				skipped++
				continue
			}
			t := e.Time.UTC()
			if cur, ok := dst[addr]; !ok || t.After(cur) {
				dst[addr] = t
			}
			merged++
		}
	}
	return merged, skipped
}

// AdaptWSBatch converts venue trade events to harvest input (same shape,
// venue-decoupling point for later DEX venues).
func AdaptWSBatch(events []hyperliquid.WSTradeEvent) []WSTradeEvent {
	batch := make([]WSTradeEvent, 0, len(events))
	for _, e := range events {
		batch = append(batch, WSTradeEvent{
			Coin: e.Coin, Time: e.Time, Buyer: e.Buyer, Seller: e.Seller,
		})
	}
	return batch
}

// WSHarvestService batches trade-stream addresses into idempotent registry
// upserts (spec §6 + §11: cheap, batched, never per-event transactions).
type WSHarvestService struct {
	repo    *Repository
	venueID uuid.UUID

	batchSize int
	interval  time.Duration
	queueCap  int
	logf      func(format string, args ...any)

	mu      sync.Mutex
	pending map[string]time.Time

	queue   chan []WSTradeEvent
	dropped atomic.Int64
	merged  atomic.Int64
	skipped atomic.Int64
	flushes atomic.Int64
}

func NewWSHarvestService(repo *Repository, venueID uuid.UUID, batchSize int, interval time.Duration) *WSHarvestService {
	if batchSize <= 0 {
		batchSize = 500
	}
	if interval <= 0 {
		interval = time.Second
	}
	return &WSHarvestService{
		repo: repo, venueID: venueID,
		batchSize: batchSize, interval: interval,
		queueCap: 4096,
		pending:  map[string]time.Time{},
		queue:    make(chan []WSTradeEvent, 4096),
		logf:     log.Printf,
	}
}

// WSHarvestStats is a point-in-time snapshot (BE-036 backpressure visible).
type WSHarvestStats struct {
	Pending int
	Dropped int64
	Merged  int64
	Skipped int64
	Flushes int64
}

func (s *WSHarvestService) Stats() WSHarvestStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return WSHarvestStats{
		Pending: len(s.pending), Dropped: s.dropped.Load(),
		Merged: s.merged.Load(), Skipped: s.skipped.Load(), Flushes: s.flushes.Load(),
	}
}

// Submit enqueues events without blocking the socket loop; when the bounded
// queue is full the incoming batch is dropped and counted (BE-036 controlled
// backpressure: drops are explicit, never silent memory growth).
func (s *WSHarvestService) Submit(events []WSTradeEvent) {
	select {
	case s.queue <- events:
	default:
		s.dropped.Add(int64(len(events)))
	}
}

// Start drains the queue, flushing on batch-size or interval until ctx ends.
func (s *WSHarvestService) Start(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	defer s.flush(context.Background())
	for {
		select {
		case <-ctx.Done():
			return
		case batch := <-s.queue:
			s.mu.Lock()
			m, sk := mergeBatch(s.pending, batch)
			s.merged.Add(int64(m))
			s.skipped.Add(int64(sk))
			full := len(s.pending) >= s.batchSize
			s.mu.Unlock()
			if full {
				if err := s.flush(ctx); err != nil {
					s.logf("ws harvest: %v", err)
				}
			}
		case <-ticker.C:
			if err := s.flush(ctx); err != nil {
				s.logf("ws harvest: %v", err)
			}
		}
	}
}

func (s *WSHarvestService) flush(ctx context.Context) error {
	s.mu.Lock()
	if len(s.pending) == 0 {
		s.mu.Unlock()
		return nil
	}
	batch := s.pending
	s.pending = map[string]time.Time{}
	s.mu.Unlock()

	addrs := make([]string, 0, len(batch))
	times := make([]time.Time, 0, len(batch))
	for a, t := range batch {
		addrs = append(addrs, a)
		times = append(times, t)
	}
	if _, err := s.repo.UpsertRegistryWS(ctx, s.venueID, addrs, times); err != nil {
		s.logf("ws harvest flush failed (%d addrs): %v", len(addrs), err)
		// Re-queue merged (bounded by pending cap via next flush cycle).
		s.mu.Lock()
		for i, a := range addrs {
			if cur, ok := s.pending[a]; !ok || times[i].After(cur) {
				s.pending[a] = times[i]
			}
		}
		s.mu.Unlock()
		return err
	}
	s.flushes.Add(1)
	return nil
}
