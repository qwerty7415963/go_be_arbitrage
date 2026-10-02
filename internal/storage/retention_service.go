package storage

import (
	"context"
	"errors"
	"fmt"
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
	TraderDaily        int64 `json:"trader_daily"`
	TraderEquity       int64 `json:"trader_equity"`
	DeadTraders        int64 `json:"dead_traders"`
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

// CleanupDeadTraders prunes registry wallets that proved empty AND went
// quiet: fully backfilled, no staged fills, no traded days, no group, and
// last seen before the threshold. Children cascade via FK; memberships are
// deleted first (no FK to registry — avoids orphans). A pruned wallet that
// trades again re-enters via discovery (self-healing; source restarts fresh).
func (s *RetentionService) CleanupDeadTraders(ctx context.Context, staleAfter time.Duration) (int64, error) {
	var n int64
	err := s.db.QueryRow(ctx, `
		WITH pruned AS (
			DELETE FROM trader_registry r
			WHERE r.status = 'active'
			  AND r.last_seen_at < NOW() - $1::interval
			  AND EXISTS (SELECT 1 FROM trader_sync_state s
				WHERE s.venue_id = r.venue_id AND s.wallet_address = r.wallet_address
				  AND s.backfill_completed_at IS NOT NULL)
			  AND NOT EXISTS (SELECT 1 FROM trader_fill_buffer f
				WHERE f.venue_id = r.venue_id AND f.wallet_address = r.wallet_address)
			  AND NOT EXISTS (SELECT 1 FROM trader_daily_stats d
				WHERE d.venue_id = r.venue_id AND d.wallet_address = r.wallet_address
				  AND d.trade_count > 0)
			  AND NOT EXISTS (SELECT 1 FROM trader_group_members m
				WHERE m.venue_id = r.venue_id AND m.wallet_address = r.wallet_address)
			RETURNING venue_id, wallet_address
		),
		del_members AS (
			DELETE FROM trader_group_members m USING pruned p
			WHERE m.venue_id = p.venue_id AND m.wallet_address = p.wallet_address
		)
		SELECT COUNT(*) FROM pruned`,
		staleAfter.String()).Scan(&n)
	return n, err
}

// CleanupTraderDaily deletes daily aggregates older than maxAge. Periods only
// read the last 365d (ALL), so anything older is never queried (RET).
func (s *RetentionService) CleanupTraderDaily(ctx context.Context, maxAge time.Duration) (int64, error) {
	query := `DELETE FROM trader_daily_stats WHERE stat_date < CURRENT_DATE - $1::interval`
	result, err := s.db.Exec(ctx, query, maxAge.String())
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

// CleanupTraderEquity deletes equity-curve days older than maxAge (drawdown is
// computed per period from the in-window curve only).
func (s *RetentionService) CleanupTraderEquity(ctx context.Context, maxAge time.Duration) (int64, error) {
	query := `DELETE FROM trader_equity_daily WHERE stat_date < CURRENT_DATE - $1::interval`
	result, err := s.db.Exec(ctx, query, maxAge.String())
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

// RunFullCleanup runs every table cleanup, continuing past per-table
// failures (a missing/unavailable table must not block the rest). Returns
// partial counts plus the joined errors, if any.
func (s *RetentionService) RunFullCleanup(ctx context.Context, config RetentionConfig) (*RetentionResult, error) {
	result := &RetentionResult{}
	var errs []error
	run := func(name string, set func(int64), fn func() (int64, error)) {
		n, err := fn()
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
			return
		}
		set(n)
	}

	run("raw_market_events", func(n int64) { result.RawMarketEvents = n },
		func() (int64, error) { return s.CleanupRawMarketEvents(ctx, config.RawMarketEventsMaxAge) })
	run("market_trades", func(n int64) { result.MarketTrades = n },
		func() (int64, error) { return s.CleanupMarketTrades(ctx, config.MarketTradesMaxAge) })
	run("market_tickers", func(n int64) { result.MarketTickers = n },
		func() (int64, error) { return s.CleanupMarketTickers(ctx, config.MarketTickersMaxAge) })
	run("funding_rates", func(n int64) { result.FundingRates = n },
		func() (int64, error) { return s.CleanupFundingRates(ctx, config.FundingRatesMaxAge) })
	run("orderbook_snapshots", func(n int64) { result.OrderbookSnapshots = n },
		func() (int64, error) { return s.CleanupOrderbookSnapshots(ctx, config.OrderbookSnapshotsMaxAge) })
	run("orderbook_deltas", func(n int64) { result.OrderbookDeltas = n },
		func() (int64, error) { return s.CleanupOrderbookDeltas(ctx, config.OrderbookDeltasMaxAge) })
	run("opportunities", func(n int64) { result.Opportunities = n },
		func() (int64, error) { return s.CleanupExpiredOpportunities(ctx) })
	run("system_events", func(n int64) { result.SystemEvents = n },
		func() (int64, error) { return s.CleanupOldSystemEvents(ctx, config.SystemEventsMaxAge) })
	run("trader_daily", func(n int64) { result.TraderDaily = n },
		func() (int64, error) { return s.CleanupTraderDaily(ctx, config.TraderDailyMaxAge) })
	run("trader_equity", func(n int64) { result.TraderEquity = n },
		func() (int64, error) { return s.CleanupTraderEquity(ctx, config.TraderEquityMaxAge) })
	if config.TraderPruneEnabled {
		run("dead_traders", func(n int64) { result.DeadTraders = n },
			func() (int64, error) { return s.CleanupDeadTraders(ctx, config.TraderPruneStaleAfter) })
	}

	return result, errors.Join(errs...)
}

type RetentionConfig struct {
	RawMarketEventsMaxAge    time.Duration
	MarketTradesMaxAge       time.Duration
	MarketTickersMaxAge      time.Duration
	FundingRatesMaxAge       time.Duration
	OrderbookSnapshotsMaxAge time.Duration
	OrderbookDeltasMaxAge    time.Duration
	SystemEventsMaxAge       time.Duration
	TraderDailyMaxAge        time.Duration
	TraderEquityMaxAge       time.Duration
	TraderPruneEnabled       bool
	TraderPruneStaleAfter    time.Duration
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
		TraderDailyMaxAge:        400 * 24 * time.Hour,
		TraderEquityMaxAge:       400 * 24 * time.Hour,
		TraderPruneEnabled:       true,
		TraderPruneStaleAfter:    30 * 24 * time.Hour,
	}
}
