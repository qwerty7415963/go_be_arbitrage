package unifiedstate

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Service struct {
	engine      *Engine
	repo        *Repository
	subscribers map[uuid.UUID][]chan *InstrumentState
	mu          sync.RWMutex
}

func NewService(repo *Repository) *Service {
	return &Service{
		engine:      NewEngine(),
		repo:        repo,
		subscribers: make(map[uuid.UUID][]chan *InstrumentState),
	}
}

func (s *Service) GetOrCreateVenueState(venueID, instrumentID uuid.UUID, venueCode string) *VenueMarketState {
	return s.engine.GetOrCreateVenueState(venueID, instrumentID, venueCode)
}

func (s *Service) GetOrCreateInstrumentState(instrumentID uuid.UUID, canonicalSymbol, baseAsset, quoteAsset string) *InstrumentState {
	return s.engine.GetOrCreateInstrumentState(instrumentID, canonicalSymbol, baseAsset, quoteAsset)
}

func (s *Service) UpdateVenueTicker(venueID, instrumentID uuid.UUID, bestBid, bestAsk, lastPrice *string, spread *string) {
	vs := s.engine.GetVenueState(venueID, instrumentID)
	if vs == nil {
		return
	}

	vs.UpdateTicker(bestBid, bestAsk, lastPrice, spread)
	s.engine.MergeVenueState(instrumentID, vs)
	s.notifySubscribers(instrumentID)
}

func (s *Service) UpdateVenueOrderBook(venueID, instrumentID uuid.UUID, bids, asks []PriceLevel, bestBid, bestAsk *string) {
	vs := s.engine.GetVenueState(venueID, instrumentID)
	if vs == nil {
		return
	}

	vs.UpdateOrderBook(bids, asks, bestBid, bestAsk)
	s.engine.MergeVenueState(instrumentID, vs)
	s.notifySubscribers(instrumentID)
}

func (s *Service) UpdateVenueFunding(venueID, instrumentID uuid.UUID, funding *FundingState) {
	vs := s.engine.GetVenueState(venueID, instrumentID)
	if vs == nil {
		return
	}

	vs.UpdateFunding(funding)
	s.engine.MergeVenueState(instrumentID, vs)
	s.notifySubscribers(instrumentID)
}

func (s *Service) UpdateVenueHealth(venueID, instrumentID uuid.UUID, health VenueHealthStatus) {
	vs := s.engine.GetVenueState(venueID, instrumentID)
	if vs == nil {
		return
	}

	vs.UpdateHealth(health)
	s.engine.MergeVenueState(instrumentID, vs)
	s.notifySubscribers(instrumentID)
}

func (s *Service) GetInstrumentState(instrumentID uuid.UUID) *InstrumentState {
	return s.engine.GetInstrumentState(instrumentID)
}

func (s *Service) GetAllInstrumentStates() map[uuid.UUID]*InstrumentState {
	s.engine.mu.RLock()
	defer s.engine.mu.RUnlock()

	result := make(map[uuid.UUID]*InstrumentState)
	for id, is := range s.engine.instruments {
		result[id] = is
	}
	return result
}

func (s *Service) GetExecutableDepth(instrumentID uuid.UUID, depth int) *ExecutableDepth {
	return s.engine.GetExecutableDepth(instrumentID, depth)
}

func (s *Service) GetHealthOverview() *HealthOverview {
	return s.engine.GetHealthOverview()
}

func (s *Service) GetSnapshot() *UnifiedStateSnapshot {
	return s.engine.GetSnapshot()
}

func (s *Service) ExcludeStaleVenues(staleThreshold time.Duration) []uuid.UUID {
	return s.engine.ExcludeStaleVenues(staleThreshold)
}

func (s *Service) Subscribe(instrumentID uuid.UUID) chan *InstrumentState {
	s.mu.Lock()
	defer s.mu.Unlock()

	ch := make(chan *InstrumentState, 100)
	s.subscribers[instrumentID] = append(s.subscribers[instrumentID], ch)
	return ch
}

func (s *Service) Unsubscribe(instrumentID uuid.UUID, ch chan *InstrumentState) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if subs, ok := s.subscribers[instrumentID]; ok {
		for i, sub := range subs {
			if sub == ch {
				s.subscribers[instrumentID] = append(subs[:i], subs[i+1:]...)
				close(ch)
				return
			}
		}
	}
}

func (s *Service) notifySubscribers(instrumentID uuid.UUID) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if subs, ok := s.subscribers[instrumentID]; ok {
		is := s.engine.GetInstrumentState(instrumentID)
		if is == nil {
			return
		}

		for _, ch := range subs {
			select {
			case ch <- is:
			default:
			}
		}
	}
}

func (s *Service) PersistState(ctx context.Context, instrumentID uuid.UUID) error {
	is := s.engine.GetInstrumentState(instrumentID)
	if is == nil {
		return nil
	}
	return s.repo.UpsertInstrumentState(ctx, is)
}

func (s *Service) PersistAllStates(ctx context.Context) error {
	s.engine.mu.RLock()
	ids := make([]uuid.UUID, 0, len(s.engine.instruments))
	for id := range s.engine.instruments {
		ids = append(ids, id)
	}
	s.engine.mu.RUnlock()

	for _, id := range ids {
		if err := s.PersistState(ctx, id); err != nil {
			return err
		}
	}
	return nil
}
