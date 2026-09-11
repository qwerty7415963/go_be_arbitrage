package storage

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type RetentionService struct {
	db *pgxpool.Pool
}

func NewRetentionService(db *pgxpool.Pool) *RetentionService {
	return &RetentionService{db: db}
}

type RetentionResult struct {
	RawMarketEvents    int64 `json:"raw_market_events"`
	MarketTrades       int64 `json:"market_trades"`
	MarketTickers      int64 `json:"market_tickers"`
	FundingRates       int64 `json:"funding_rates"`
	OrderbookSnapshots int64 `json:"orderbook_snapshots"`
	OrderbookDeltas    int64 `json:"orderbook_deltas"`
	Opportunities      int64 `json:"opportunities"`
	SystemEvents       int64 `json:"system_events"`
}

func (s *RetentionService) CleanupRawMarketEvents(ctx context.Context, maxAge time.Duration) (int64, error) {
	query := `DELETE FROM raw_market_events WHERE receive_timestamp < NOW() - $1::interval`
	result, err := s.db.Exec(ctx, query, maxAge.String())
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

func (s *RetentionService) CleanupMarketTrades(ctx context.Context, maxAge time.Duration) (int64, error) {
	query := `DELETE FROM market_trades WHERE receive_timestamp < NOW() - $1::interval`
	result, err := s.db.Exec(ctx, query, maxAge.String())
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

func (s *RetentionService) CleanupMarketTickers(ctx context.Context, maxAge time.Duration) (int64, error) {
	query := `DELETE FROM market_tickers WHERE receive_timestamp < NOW() - $1::interval`
	result, err := s.db.Exec(ctx, query, maxAge.String())
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

func (s *RetentionService) CleanupFundingRates(ctx context.Context, maxAge time.Duration) (int64, error) {
	query := `DELETE FROM funding_rates WHERE observed_at < NOW() - $1::interval`
	result, err := s.db.Exec(ctx, query, maxAge.String())
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

func (s *RetentionService) CleanupOrderbookSnapshots(ctx context.Context, maxAge time.Duration) (int64, error) {
	query := `DELETE FROM orderbook_snapshots WHERE timestamp < NOW() - $1::interval`
	result, err := s.db.Exec(ctx, query, maxAge.String())
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

func (s *RetentionService) CleanupOrderbookDeltas(ctx context.Context, maxAge time.Duration) (int64, error) {
	query := `DELETE FROM orderbook_deltas WHERE timestamp < NOW() - $1::interval`
	result, err := s.db.Exec(ctx, query, maxAge.String())
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

func (s *RetentionService) CleanupExpiredOpportunities(ctx context.Context) (int64, error) {
	query := `DELETE FROM opportunities WHERE expires_at < NOW()`
	result, err := s.db.Exec(ctx, query)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

func (s *RetentionService) CleanupOldSystemEvents(ctx context.Context, maxAge time.Duration) (int64, error) {
	query := `DELETE FROM system_events WHERE occurred_at < NOW() - $1::interval`
	result, err := s.db.Exec(ctx, query, maxAge.String())
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

func (s *RetentionService) RunFullCleanup(ctx context.Context, config RetentionConfig) (*RetentionResult, error) {
	result := &RetentionResult{}

	var err error

	result.RawMarketEvents, err = s.CleanupRawMarketEvents(ctx, config.RawMarketEventsMaxAge)
	if err != nil {
		return nil, err
	}

	result.MarketTrades, err = s.CleanupMarketTrades(ctx, config.MarketTradesMaxAge)
	if err != nil {
		return nil, err
	}

	result.MarketTickers, err = s.CleanupMarketTickers(ctx, config.MarketTickersMaxAge)
	if err != nil {
		return nil, err
	}

	result.FundingRates, err = s.CleanupFundingRates(ctx, config.FundingRatesMaxAge)
	if err != nil {
		return nil, err
	}

	result.OrderbookSnapshots, err = s.CleanupOrderbookSnapshots(ctx, config.OrderbookSnapshotsMaxAge)
	if err != nil {
		return nil, err
	}

	result.OrderbookDeltas, err = s.CleanupOrderbookDeltas(ctx, config.OrderbookDeltasMaxAge)
	if err != nil {
		return nil, err
	}

	result.Opportunities, err = s.CleanupExpiredOpportunities(ctx)
	if err != nil {
		return nil, err
	}

	result.SystemEvents, err = s.CleanupOldSystemEvents(ctx, config.SystemEventsMaxAge)
	if err != nil {
		return nil, err
	}

	return result, nil
}

type RetentionConfig struct {
	RawMarketEventsMaxAge    time.Duration
	MarketTradesMaxAge       time.Duration
	MarketTickersMaxAge      time.Duration
	FundingRatesMaxAge       time.Duration
	OrderbookSnapshotsMaxAge time.Duration
	OrderbookDeltasMaxAge    time.Duration
	SystemEventsMaxAge       time.Duration
}

func DefaultRetentionConfig() RetentionConfig {
	return RetentionConfig{
		RawMarketEventsMaxAge:    7 * 24 * time.Hour,
		MarketTradesMaxAge:       30 * 24 * time.Hour,
		MarketTickersMaxAge:      30 * 24 * time.Hour,
		FundingRatesMaxAge:       90 * 24 * time.Hour,
		OrderbookSnapshotsMaxAge: 7 * 24 * time.Hour,
		OrderbookDeltasMaxAge:    7 * 24 * time.Hour,
		SystemEventsMaxAge:       90 * 24 * time.Hour,
	}
}
