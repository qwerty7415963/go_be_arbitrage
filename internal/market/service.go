package market

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

type Service struct {
	rawRepo      *RawEventRepository
	normalizedRepo *NormalizedEventRepository
	connManager  *ConnectionManager
	subManager   *SubscriptionManager
}

func NewService(
	rawRepo *RawEventRepository,
	normalizedRepo *NormalizedEventRepository,
	connManager *ConnectionManager,
	subManager *SubscriptionManager,
) *Service {
	return &Service{
		rawRepo:      rawRepo,
		normalizedRepo: normalizedRepo,
		connManager:  connManager,
		subManager:   subManager,
	}
}

func (s *Service) ProcessRawEvent(ctx context.Context, event *RawMarketEvent) error {
	// Store raw event
	if err := s.rawRepo.Create(ctx, event); err != nil {
		return domain.WrapError(domain.ErrCodeMarketEventOutOfOrder, "failed to store raw event", err)
	}

	return nil
}

func (s *Service) ProcessTrade(ctx context.Context, trade *TradeEvent, rawEventID *int64) error {
	// Store normalized trade
	if err := s.normalizedRepo.CreateTrade(ctx, trade); err != nil {
		return domain.WrapError(domain.ErrCodeMarketDuplicateEvent, "failed to store trade", err)
	}

	return nil
}

func (s *Service) ProcessTicker(ctx context.Context, ticker *TickerEvent, rawEventID *int64) error {
	// Store normalized ticker
	if err := s.normalizedRepo.CreateTicker(ctx, ticker); err != nil {
		return domain.WrapError(domain.ErrCodeMarketDuplicateEvent, "failed to store ticker", err)
	}

	return nil
}

func (s *Service) ProcessFunding(ctx context.Context, funding *FundingEvent, rawEventID *int64) error {
	// Store normalized funding
	if err := s.normalizedRepo.CreateFunding(ctx, funding); err != nil {
		return domain.WrapError(domain.ErrCodeMarketDuplicateEvent, "failed to store funding", err)
	}

	return nil
}

func (s *Service) GetLatestTicker(ctx context.Context, venueID, instrumentID uuid.UUID) (*TickerEvent, error) {
	ticker, err := s.normalizedRepo.GetLatestTicker(ctx, venueID, instrumentID)
	if err != nil {
		return nil, domain.WrapError(domain.ErrCodeMarketTickerUnavailable, "failed to get ticker", err)
	}
	return ticker, nil
}

func (s *Service) GetLatestFunding(ctx context.Context, venueID, instrumentID uuid.UUID) (*FundingEvent, error) {
	funding, err := s.normalizedRepo.GetLatestFunding(ctx, venueID, instrumentID)
	if err != nil {
		return nil, domain.WrapError(domain.ErrCodeMarketFundingMissing, "failed to get funding", err)
	}
	return funding, nil
}

func (s *Service) GetRecentTrades(ctx context.Context, venueID, instrumentID uuid.UUID, limit int) ([]*TradeEvent, error) {
	trades, err := s.normalizedRepo.ListTradesByVenueInstrument(ctx, venueID, instrumentID, limit)
	if err != nil {
		return nil, domain.WrapError(domain.ErrCodeMarketUnavailable, "failed to get trades", err)
	}
	return trades, nil
}

func (s *Service) Subscribe(ctx context.Context, venueID, instrumentID uuid.UUID, channel string, connectionID uuid.UUID) (*Subscription, error) {
	sub := s.subManager.Subscribe(venueID, instrumentID, channel, connectionID)
	s.subManager.UpdateStatus(sub.ID, SubscriptionStatusActive)
	return sub, nil
}

func (s *Service) Unsubscribe(ctx context.Context, subscriptionID uuid.UUID) error {
	s.subManager.Unsubscribe(subscriptionID)
	return nil
}

func (s *Service) GetActiveSubscriptions() []*Subscription {
	return s.subManager.GetActiveSubscriptions()
}

func (s *Service) GetVenueSubscriptions(venueID uuid.UUID) []*Subscription {
	return s.subManager.GetVenueSubscriptions(venueID)
}

func (s *Service) CreateConnection(venueCode string) *Connection {
	return s.connManager.CreateConnection(venueCode)
}

func (s *Service) GetConnection(id uuid.UUID) (*Connection, bool) {
	return s.connManager.GetConnection(id)
}

func (s *Service) IsConnected(id uuid.UUID) bool {
	return s.connManager.IsConnected(id)
}

func (s *Service) CleanupOldEvents(ctx context.Context, maxAgeDays int) (int64, error) {
	return s.rawRepo.DeleteByAge(ctx, maxAgeDays)
}

func ParseTradeEvent(data []byte) (*TradeEvent, error) {
	var event TradeEvent
	if err := json.Unmarshal(data, &event); err != nil {
		return nil, err
	}
	return &event, nil
}

func ParseTickerEvent(data []byte) (*TickerEvent, error) {
	var event TickerEvent
	if err := json.Unmarshal(data, &event); err != nil {
		return nil, err
	}
	return &event, nil
}

func ParseFundingEvent(data []byte) (*FundingEvent, error) {
	var event FundingEvent
	if err := json.Unmarshal(data, &event); err != nil {
		return nil, err
	}
	return &event, nil
}

func NormalizeTimestamp(ts int64) time.Time {
	// Handle both seconds and milliseconds
	if ts > 1e12 {
		// Milliseconds
		return time.UnixMilli(ts)
	}
	// Seconds
	return time.Unix(ts, 0)
}
