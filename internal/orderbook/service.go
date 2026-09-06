package orderbook

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Service struct {
	engine    *Engine
	repo      *Repository
	subscribers map[string][]chan *OrderBookSnapshot
	mu          sync.RWMutex
}

func NewService(repo *Repository) *Service {
	return &Service{
		engine:      NewEngine(),
		repo:        repo,
		subscribers: make(map[string][]chan *OrderBookSnapshot),
	}
}

func (s *Service) GetOrCreateBook(venueID, instrumentID uuid.UUID) *OrderBook {
	return s.engine.GetOrCreateBook(venueID, instrumentID, 5*time.Second, 10*time.Second)
}

func (s *Service) ApplySnapshot(ctx context.Context, snapshot *OrderBookSnapshot) error {
	if !ValidateSnapshot(snapshot) {
		return ErrInvalidDepth
	}

	book := s.engine.GetOrCreateBook(snapshot.VenueID, snapshot.InstrumentID, 5*time.Second, 10*time.Second)

	if err := s.engine.ApplySnapshot(book, snapshot); err != nil {
		return err
	}

	if err := s.repo.CreateSnapshot(ctx, snapshot); err != nil {
		return err
	}

	s.notifySubscribers(snapshot)

	return nil
}

func (s *Service) ApplyDelta(ctx context.Context, delta *OrderBookDelta) error {
	if !ValidateDelta(delta) {
		return ErrDeltaTooOld
	}

	book := s.engine.GetBook(delta.VenueID, delta.InstrumentID)
	if book == nil {
		return ErrBookNotFound
	}

	if err := s.engine.ApplyDelta(book, delta); err != nil {
		return err
	}

	if err := s.repo.CreateDelta(ctx, delta); err != nil {
		return err
	}

	return nil
}

func (s *Service) GetBook(venueID, instrumentID uuid.UUID) *OrderBook {
	return s.engine.GetBook(venueID, instrumentID)
}

func (s *Service) GetHealth(venueID, instrumentID uuid.UUID) (*OrderBookHealth, error) {
	book := s.engine.GetBook(venueID, instrumentID)
	if book == nil {
		return nil, ErrBookNotFound
	}

	return s.engine.GetHealth(book), nil
}

func (s *Service) GetDepth(venueID, instrumentID uuid.UUID, depth int) (*OrderBookDepth, error) {
	book := s.engine.GetBook(venueID, instrumentID)
	if book == nil {
		return nil, ErrBookNotFound
	}

	if depth <= 0 || depth > 20 {
		return nil, ErrInvalidDepth
	}

	return s.engine.GetDepth(book, depth), nil
}

func (s *Service) IsTradable(venueID, instrumentID uuid.UUID) bool {
	book := s.engine.GetBook(venueID, instrumentID)
	if book == nil {
		return false
	}

	return s.engine.IsTradable(book)
}

func (s *Service) RequestResync(venueID, instrumentID uuid.UUID) error {
	book := s.engine.GetBook(venueID, instrumentID)
	if book == nil {
		return ErrBookNotFound
	}

	s.engine.RequestResync(book)
	return nil
}

func (s *Service) Subscribe(venueID, instrumentID uuid.UUID) chan *OrderBookSnapshot {
	key := s.engine.GetBookKey(venueID, instrumentID)

	s.mu.Lock()
	defer s.mu.Unlock()

	ch := make(chan *OrderBookSnapshot, 100)
	s.subscribers[key] = append(s.subscribers[key], ch)

	return ch
}

func (s *Service) Unsubscribe(venueID, instrumentID uuid.UUID, ch chan *OrderBookSnapshot) {
	key := s.engine.GetBookKey(venueID, instrumentID)

	s.mu.Lock()
	defer s.mu.Unlock()

	if subs, ok := s.subscribers[key]; ok {
		for i, sub := range subs {
			if sub == ch {
				s.subscribers[key] = append(subs[:i], subs[i+1:]...)
				close(ch)
				return
			}
		}
	}
}

func (s *Service) notifySubscribers(snapshot *OrderBookSnapshot) {
	key := s.engine.GetBookKey(snapshot.VenueID, snapshot.InstrumentID)

	s.mu.RLock()
	defer s.mu.RUnlock()

	if subs, ok := s.subscribers[key]; ok {
		for _, ch := range subs {
			select {
			case ch <- snapshot:
			default:
			}
		}
	}
}

func (s *Service) CleanupOldData(ctx context.Context, maxAge time.Duration) (int64, int64, error) {
	snapshots, err := s.repo.DeleteOldSnapshots(ctx, maxAge)
	if err != nil {
		return 0, 0, err
	}

	deltas, err := s.repo.DeleteOldDeltas(ctx, maxAge)
	if err != nil {
		return snapshots, 0, err
	}

	return snapshots, deltas, nil
}
