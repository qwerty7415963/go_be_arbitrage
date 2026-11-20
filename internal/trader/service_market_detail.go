package trader

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/qwerty7415963/go_be_arbitrage/internal/api"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

// MarketDetail returns market detail with caching (30s).
func (s *Service) MarketDetail(ctx context.Context, venueCode, market string, from, to time.Time) (*MarketDetailResponse, error) {
	venue := strings.ToLower(strings.TrimSpace(venueCode))
	if venue == "" {
		venue = DefaultVenue
	}
	market = strings.ToUpper(strings.TrimSpace(market))
	if market == "" {
		return nil, domain.NewError(domain.ErrCodeInvalidFilter, "market is required")
	}
	venueID, err := s.repo.VenueIDByCode(ctx, venue)
	if err != nil {
		return nil, domain.NewError(domain.ErrCodeInvalidFilter, "unknown venue")
	}

	cacheKey := "md|" + venue + "|" + market
	s.marketDetailMu.Lock()
	c, ok := s.marketDetailCache[cacheKey]
	expired := ok && time.Now().After(c.exp)
	s.marketDetailMu.Unlock()
	if ok && !expired {
		v := c.val
		return &v, nil
	}

	header, err := s.repo.GetMarketHeader(ctx, venueID, market)
	if err != nil {
		return nil, err
	}
	if header.Venue == "" {
		header.Venue = venue
	}
	header.Market = market
	if header.AsOf == "" {
		header.AsOf = time.Now().UTC().Format(time.RFC3339)
	}
	snapshot, err := s.repo.GetMarketSnapshot(ctx, venueID, market)
	if err != nil {
		return nil, err
	}
	if snapshot.SyncedAt == "" && snapshot.TradersTotal > 0 {
		snapshot.SyncedAt = header.AsOf
	}

	// coverage
	tracked := 0
	total := 0
	r1, _ := s.repo.poolQueryInt(ctx, `SELECT COUNT(DISTINCT wallet_address)::int FROM trader_positions WHERE venue_id=$1 AND coin=$2`, venueID, market)
	tracked = r1
	r2, _ := s.repo.poolQueryInt(ctx, `SELECT COUNT(DISTINCT wallet_address)::int FROM trader_trades WHERE venue_id=$1 AND market=$2`, venueID, market)
	total = r2

	resp := MarketDetailResponse{
		Header:   *header,
		Snapshot: *snapshot,
		Coverage: MarketDetailCoverage{
			TrackedWalletsWithPositions: tracked,
			TotalWalletsInMarket:        total,
		},
	}
	s.marketDetailMu.Lock()
	s.marketDetailCache[cacheKey] = cachedMarketDetail{val: resp, exp: time.Now().Add(30 * time.Second)}
	s.marketDetailMu.Unlock()
	return &resp, nil
}

// MarketWallets returns paginated market wallets with caching (5min).
func (s *Service) MarketWallets(ctx context.Context, venueCode, market string, from, to time.Time, winRateMin *float64, tradesMin *int, lastTradeAfter *time.Time, groups []uuid.UUID, sort, dir string, page, limit int) (*MarketWalletsResponse, error) {
	venue := strings.ToLower(strings.TrimSpace(venueCode))
	if venue == "" {
		venue = DefaultVenue
	}
	market = strings.ToUpper(strings.TrimSpace(market))
	if market == "" {
		return nil, domain.NewError(domain.ErrCodeInvalidFilter, "market is required")
	}
	if sort == "" {
		sort = "volume"
	}
	if dir == "" {
		dir = "desc"
	}
	switch sort {
	case "volume", "pnl", "trades", "win_rate", "last_trade", "open_notional", "open_upnl", "leverage":
	default:
		return nil, domain.NewError(domain.ErrCodeInvalidFilter, "invalid sort")
	}
	if dir != "asc" && dir != "desc" {
		return nil, domain.NewError(domain.ErrCodeInvalidFilter, "invalid dir")
	}
	if winRateMin != nil && (*winRateMin < 0 || *winRateMin > 100) {
		return nil, domain.NewError(domain.ErrCodeInvalidFilter, "win_rate_min must be 0..100")
	}
	if tradesMin != nil && *tradesMin < 0 {
		return nil, domain.NewError(domain.ErrCodeInvalidFilter, "trades_min must be >= 0")
	}
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	venueID, err := s.repo.VenueIDByCode(ctx, venue)
	if err != nil {
		return nil, domain.NewError(domain.ErrCodeInvalidFilter, "unknown venue")
	}

	cacheKey := fmt.Sprintf("mw|%s|%s|%v|%v|%v|%v|%v|%v|%s|%s|%d|%d", venue, market, from, to, winRateMin, tradesMin, lastTradeAfter, groups, sort, dir, page, limit)
	s.marketWalletsMu.Lock()
	c, ok := s.marketWalletsCache[cacheKey]
	expired := ok && time.Now().After(c.exp)
	s.marketWalletsMu.Unlock()
	if ok && !expired {
		rows := append([]MarketWalletRow(nil), c.val...)
		total := c.total
		tp := (total + limit - 1) / limit
		if tp == 0 {
			tp = 1
		}
		return &MarketWalletsResponse{Data: rows, Meta: &api.Meta{Page: page, Total: int64(total), TotalPages: tp, Limit: limit, HasMore: page < tp}}, nil
	}

	rows, total, err := s.repo.GetMarketWallets(ctx, venueID, market, from, to, winRateMin, tradesMin, lastTradeAfter, groups, sort, dir, page, limit)
	if err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []MarketWalletRow{}
	}
	s.marketWalletsMu.Lock()
	s.marketWalletsCache[cacheKey] = cachedMarketWallets{val: append([]MarketWalletRow(nil), rows...), total: total, exp: time.Now().Add(5 * time.Minute)}
	s.marketWalletsMu.Unlock()

	tp := (total + limit - 1) / limit
	if tp == 0 {
		tp = 1
	}
	return &MarketWalletsResponse{Data: rows, Meta: &api.Meta{Page: page, Total: int64(total), TotalPages: tp, Limit: limit, HasMore: page < tp}}, nil
}

// MarketTrades returns paginated market trades with caching (5min).
func (s *Service) MarketTrades(ctx context.Context, venueCode, market string, from, to time.Time, side string, page, limit int) (*MarketTradesResponse, error) {
	venue := strings.ToLower(strings.TrimSpace(venueCode))
	if venue == "" {
		venue = DefaultVenue
	}
	market = strings.ToUpper(strings.TrimSpace(market))
	if market == "" {
		return nil, domain.NewError(domain.ErrCodeInvalidFilter, "market is required")
	}
	side = strings.ToUpper(strings.TrimSpace(side))
	if side == "" {
		side = "ALL"
	}
	if side != "ALL" && side != "LONG" && side != "SHORT" {
		return nil, domain.NewError(domain.ErrCodeInvalidFilter, "invalid side")
	}
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	venueID, err := s.repo.VenueIDByCode(ctx, venue)
	if err != nil {
		return nil, domain.NewError(domain.ErrCodeInvalidFilter, "unknown venue")
	}

	cacheKey := fmt.Sprintf("mt|%s|%s|%v|%v|%s|%d|%d", venue, market, from, to, side, page, limit)
	s.marketTradesMu.Lock()
	c, ok := s.marketTradesCache[cacheKey]
	expired := ok && time.Now().After(c.exp)
	s.marketTradesMu.Unlock()
	if ok && !expired {
		rows := append([]MarketTradeRow(nil), c.val...)
		total := c.total
		tp := (total + limit - 1) / limit
		if tp == 0 {
			tp = 1
		}
		return &MarketTradesResponse{Data: rows, Meta: &api.Meta{Page: page, Total: int64(total), TotalPages: tp, Limit: limit, HasMore: page < tp}}, nil
	}

	rows, total, err := s.repo.GetMarketTrades(ctx, venueID, market, from, to, side, page, limit)
	if err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []MarketTradeRow{}
	}
	s.marketTradesMu.Lock()
	s.marketTradesCache[cacheKey] = cachedMarketTrades{val: append([]MarketTradeRow(nil), rows...), total: total, exp: time.Now().Add(5 * time.Minute)}
	s.marketTradesMu.Unlock()

	tp := (total + limit - 1) / limit
	if tp == 0 {
		tp = 1
	}
	return &MarketTradesResponse{Data: rows, Meta: &api.Meta{Page: page, Total: int64(total), TotalPages: tp, Limit: limit, HasMore: page < tp}}, nil
}

// WatchedMarkets CRUD service methods.
func (s *Service) ListWatchedMarkets(ctx context.Context) ([]WatchedMarket, error) {
	return s.repo.ListWatchedMarkets(ctx)
}

func (s *Service) CreateWatchedMarket(ctx context.Context, venueCode, market string, enabled bool, sortOrder int) error {
	venue := strings.ToLower(strings.TrimSpace(venueCode))
	if venue == "" {
		venue = DefaultVenue
	}
	market = strings.ToUpper(strings.TrimSpace(market))
	venueID, err := s.repo.VenueIDByCode(ctx, venue)
	if err != nil {
		return domain.NewError(domain.ErrCodeInvalidFilter, "unknown venue")
	}
	return s.repo.CreateWatchedMarket(ctx, venueID, market, enabled, sortOrder)
}

func (s *Service) UpdateWatchedMarket(ctx context.Context, venueCode, market string, enabled *bool, sortOrder *int) error {
	venue := strings.ToLower(strings.TrimSpace(venueCode))
	if venue == "" {
		venue = DefaultVenue
	}
	market = strings.ToUpper(strings.TrimSpace(market))
	venueID, err := s.repo.VenueIDByCode(ctx, venue)
	if err != nil {
		return domain.NewError(domain.ErrCodeInvalidFilter, "unknown venue")
	}
	return s.repo.UpdateWatchedMarket(ctx, venueID, market, enabled, sortOrder)
}

func (s *Service) DeleteWatchedMarket(ctx context.Context, venueCode, market string) error {
	venue := strings.ToLower(strings.TrimSpace(venueCode))
	if venue == "" {
		venue = DefaultVenue
	}
	market = strings.ToUpper(strings.TrimSpace(market))
	venueID, err := s.repo.VenueIDByCode(ctx, venue)
	if err != nil {
		return domain.NewError(domain.ErrCodeInvalidFilter, "unknown venue")
	}
	return s.repo.DeleteWatchedMarket(ctx, venueID, market)
}

// poolQueryInt is a helper; avoid import cycle.
func (r *Repository) poolQueryInt(ctx context.Context, q string, args ...any) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, q, args...).Scan(&n)
	if err != nil {
		if err == pgx.ErrNoRows {
			return 0, nil
		}
		return 0, err
	}
	return n, nil
}
