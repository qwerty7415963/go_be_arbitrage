package trader

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

// GetMarketHeader loads the latest funding rate + asset logo for a market.
func (r *Repository) GetMarketHeader(ctx context.Context, venueID uuid.UUID, market string) (*MarketDetailHeader, error) {
	h := &MarketDetailHeader{Venue: "", Market: market, Enabled: false}
	// venue code
	var venueCode string
	if err := r.pool.QueryRow(ctx, `SELECT code FROM venues WHERE id=$1`, venueID).Scan(&venueCode); err != nil {
		return nil, err
	}
	h.Venue = venueCode

	var markPrice, indexPrice, fundingRate, openInterest *string
	var nextFunding *time.Time
	var observedAt *time.Time
	var logoURL, category *string
	var enabled *bool

	q := `
		SELECT
		  fr.mark_price, fr.index_price, fr.funding_rate, fr.next_funding_at, fr.open_interest, fr.observed_at,
		  aa_logo.url, a.asset_class, wm.enabled
		FROM venues v
		LEFT JOIN venue_instruments vi ON vi.venue_id = v.id AND vi.venue_symbol = $2
		LEFT JOIN LATERAL (
		  SELECT mark_price, index_price, funding_rate, next_funding_at, open_interest, observed_at
		  FROM funding_rates
		  WHERE venue_id = v.id AND instrument_id = vi.instrument_id
		  ORDER BY observed_at DESC LIMIT 1
		) fr ON TRUE
		LEFT JOIN asset_aliases aa ON aa.venue_id = v.id AND aa.venue_symbol = $2
		LEFT JOIN assets a ON a.id = aa.asset_id
		LEFT JOIN asset_logos aa_logo ON aa_logo.asset_id = a.id
		LEFT JOIN watched_markets wm ON wm.venue_id = v.id AND wm.market = $2
		WHERE v.id = $1`
	row := r.pool.QueryRow(ctx, q, venueID, market)
	err := row.Scan(&markPrice, &indexPrice, &fundingRate, &nextFunding, &openInterest, &observedAt,
		&logoURL, &category, &enabled)
	if err != nil {
		if err == pgx.ErrNoRows {
			return h, nil
		}
		return nil, err
	}
	h.MarkPrice = parseFloatPtrPtr(markPrice)
	h.IndexPrice = parseFloatPtrPtr(indexPrice)
	h.FundingRate = parseFloatPtrPtr(fundingRate)
	h.NextFundingAt = nextFunding
	h.MarketOI = parseFloatPtrPtr(openInterest)
	if logoURL != nil {
		h.LogoURL = *logoURL
	}
	if category != nil {
		h.Category = *category
	}
	if enabled != nil {
		h.Enabled = *enabled
	}
	if observedAt != nil {
		h.AsOf = observedAt.UTC().Format(time.RFC3339)
	} else {
		h.AsOf = time.Now().UTC().Format(time.RFC3339)
	}
	return h, nil
}

// GetMarketSnapshot aggregates trader_positions for a market.
func (r *Repository) GetMarketSnapshot(ctx context.Context, venueID uuid.UUID, market string) (*MarketSnapshot, error) {
	snap := &MarketSnapshot{SyncedAt: ""}
	// aggregate by side
	rows, err := r.pool.Query(ctx, `
		SELECT side,
		  COALESCE(SUM(position_value), 0) AS notional,
		  COALESCE(SUM(unrealized_pnl), 0) AS upnl,
		  COUNT(*)::int AS traders,
		  COALESCE(SUM(margin_used), 0) AS margin,
		  AVG(leverage) AS avg_lev
		FROM trader_positions
		WHERE venue_id = $1 AND coin = $2
		GROUP BY side`, venueID, market)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var longNotional, shortNotional float64
	var longUpnl, shortUpnl float64
	var longTraders, shortTraders int
	var longMargin, shortMargin float64
	var longLev, shortLev *float64
	var totalMargin float64
	hasMargin := false
	for rows.Next() {
		var side string
		var notional, upnl, margin float64
		var traders int
		var avgLev *float64
		if err := rows.Scan(&side, &notional, &upnl, &traders, &margin, &avgLev); err != nil {
			return nil, err
		}
		if side == "LONG" {
			longNotional = notional
			longUpnl = upnl
			longTraders = traders
			longMargin = margin
			longLev = avgLev
		} else {
			shortNotional = notional
			shortUpnl = upnl
			shortTraders = traders
			shortMargin = margin
			shortLev = avgLev
		}
		totalMargin += margin
		hasMargin = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	snap.OpenNotionalLong = longNotional
	snap.OpenNotionalShort = shortNotional
	if shortNotional != 0 {
		snap.LongShortRatio = longNotional / shortNotional
	} else if longNotional != 0 {
		snap.LongShortRatio = longNotional
	} else {
		snap.LongShortRatio = 0
	}
	snap.UnrealizedPnlLong = longUpnl
	snap.UnrealizedPnlShort = shortUpnl
	net := longUpnl + shortUpnl
	snap.NetUnrealizedPnl = &net
	snap.TradersLong = longTraders
	snap.TradersShort = shortTraders
	snap.TradersTotal = longTraders + shortTraders
	snap.MarginUsedLong = longMargin
	snap.MarginUsedShort = shortMargin
	snap.MarginUsedTotal = &totalMargin
	snap.AvgLeverageLong = longLev
	snap.AvgLeverageShort = shortLev

	// market OI from latest funding_rates
	var oi *string
	err = r.pool.QueryRow(ctx, `
		SELECT fr.open_interest
		FROM funding_rates fr
		JOIN venue_instruments vi ON vi.instrument_id = fr.instrument_id AND vi.venue_id = fr.venue_id
		WHERE fr.venue_id = $1 AND vi.venue_symbol = $2
		ORDER BY fr.observed_at DESC LIMIT 1`, venueID, market).Scan(&oi)
	if err != nil && err != pgx.ErrNoRows {
		return nil, err
	}
	snap.MarketOI = parseFloatPtrPtr(oi)

	// synced_at: max updated_at of trader_positions for market
	var synced *time.Time
	err = r.pool.QueryRow(ctx, `
		SELECT MAX(updated_at) FROM trader_positions WHERE venue_id=$1 AND coin=$2`, venueID, market).Scan(&synced)
	if err != nil {
		return nil, err
	}
	if synced != nil {
		snap.SyncedAt = synced.UTC().Format(time.RFC3339)
	}
	_ = hasMargin
	return snap, nil
}

// GetMarketWallets aggregates trader_trades by wallet for a market.
func (r *Repository) GetMarketWallets(ctx context.Context, venueID uuid.UUID, market string, from, to time.Time, winRateMin *float64, tradesMin *int, lastTradeAfter *time.Time, groups []uuid.UUID, sort, dir string, page, limit int) ([]MarketWalletRow, int, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	offset := (page - 1) * limit

	// Build WHERE for agg
	baseWhere := `t.venue_id = $1 AND t.market = $2 AND t.closed_at >= $3 AND t.closed_at <= $4`

	// args order:
	// $1 venueID, $2 market, $3 from, $4 to, $5 winRateMin (or nil), $6 tradesMin (or nil), $7 lastTradeAfter (or nil), $8 groups (or nil), $9? We'll restructure.
	// To simplify, pass groups as $8 and winRateMin etc nullable.
	// Let's just build dynamic args slice.

	args := []any{venueID, market, from, to}
	// placeholders for optional filters will be appended
	winIdx := 0
	if winRateMin != nil {
		winIdx = len(args) + 1
		args = append(args, *winRateMin)
	}
	tradesIdx := 0
	if tradesMin != nil {
		tradesIdx = len(args) + 1
		args = append(args, *tradesMin)
	}
	lastIdx := 0
	if lastTradeAfter != nil {
		lastIdx = len(args) + 1
		args = append(args, *lastTradeAfter)
	}
	groupsIdx := 0
	if len(groups) > 0 {
		groupsIdx = len(args) + 1
		args = append(args, groups)
	}

	// Build filter clauses with correct placeholders
	clauses := ""
	if winRateMin != nil {
		clauses += fmt.Sprintf(" AND (CASE WHEN agg.trades > 0 THEN 100.0 * agg.wins / agg.trades ELSE 0 END) >= $%d", winIdx)
	}
	if tradesMin != nil {
		clauses += fmt.Sprintf(" AND agg.trades >= $%d", tradesIdx)
	}
	if lastTradeAfter != nil {
		clauses += fmt.Sprintf(" AND agg.last_trade_at >= $%d", lastIdx)
	}
	if len(groups) > 0 {
		clauses += fmt.Sprintf(" AND EXISTS (SELECT 1 FROM trader_group_members m WHERE m.venue_id = $1 AND m.wallet_address = agg.wallet_address AND m.group_id = ANY($%d))", groupsIdx)
	}

	aggSQL := fmt.Sprintf(`
		SELECT t.wallet_address,
		  COALESCE(SUM(t.volume), 0) AS volume,
		  COUNT(*)::int AS trades,
		  COALESCE(SUM(t.pnl), 0) AS realized_pnl,
		  COUNT(*) FILTER (WHERE t.pnl > 0)::int AS wins,
		  COUNT(*) FILTER (WHERE t.side = 'LONG')::bigint AS long_count,
		  COUNT(*) FILTER (WHERE t.side = 'SHORT')::bigint AS short_count,
		  MAX(t.closed_at) AS last_trade_at
		FROM trader_trades t
		WHERE %s
		GROUP BY t.wallet_address`, baseWhere)

	// total count
	countQuery := fmt.Sprintf(`
		WITH agg AS (%s),
		filtered AS (
		  SELECT agg.wallet_address
		  FROM agg
		  JOIN trader_registry r ON r.venue_id = $1 AND r.wallet_address = agg.wallet_address
		  WHERE TRUE %s
		)
		SELECT COUNT(*)::int FROM filtered`, aggSQL, clauses)

	var total int
	if err := r.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// sort mapping
	sortCol := "volume"
	switch sort {
	case "pnl":
		sortCol = "realized_pnl"
	case "trades":
		sortCol = "trades"
	case "win_rate":
		sortCol = "CASE WHEN trades > 0 THEN 100.0 * wins / trades ELSE 0 END"
	case "last_trade":
		sortCol = "last_trade_at"
	case "open_notional":
		sortCol = "position_notional"
	case "open_upnl":
		sortCol = "position_upnl"
	case "leverage":
		sortCol = "position_leverage"
	case "volume":
		sortCol = "volume"
	}
	dirSQL := "DESC"
	if strings.ToLower(dir) == "asc" {
		dirSQL = "ASC"
	}

	dataQuery := fmt.Sprintf(`
		WITH agg AS (%s),
		filtered AS (
		  SELECT agg.wallet_address, agg.volume, agg.trades, agg.realized_pnl, agg.wins,
		    agg.long_count, agg.short_count, agg.last_trade_at,
		    r.display_name,
		    tp.side AS position_side, tp.position_value AS position_notional,
		    tp.unrealized_pnl AS position_upnl, tp.leverage AS position_leverage,
		    tp.margin_used,
		    (SELECT end_equity FROM trader_equity_daily e WHERE e.venue_id=$1 AND e.wallet_address=agg.wallet_address ORDER BY stat_date DESC LIMIT 1) AS end_equity
		  FROM agg
		  JOIN trader_registry r ON r.venue_id = $1 AND r.wallet_address = agg.wallet_address
		  LEFT JOIN trader_positions tp ON tp.venue_id = $1 AND tp.wallet_address = agg.wallet_address AND tp.coin = $2
		  WHERE TRUE %s
		)
		SELECT wallet_address, display_name, volume, trades, realized_pnl, wins,
		  long_count, short_count, last_trade_at, position_side, position_notional,
		  position_upnl, position_leverage, margin_used, end_equity
		FROM filtered
		ORDER BY %s %s NULLS LAST, wallet_address ASC
		LIMIT $%d OFFSET $%d`, aggSQL, clauses, sortCol, dirSQL, len(args)+1, len(args)+2)

	args = append(args, limit, offset)
	rows, err := r.pool.Query(ctx, dataQuery, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []MarketWalletRow
	for rows.Next() {
		var row MarketWalletRow
		var displayName *string
		var wins int
		var longCount, shortCount int64
		var lastTrade *time.Time
		var posSide *string
		var posNotional, posUpnl, posLev, marginUsed, equity *float64 // need scan types
		// We'll scan into appropriate types; position values are numeric -> *float64 via pgx? pgx can scan numeric to *float64 yes.
		if err := rows.Scan(
			&row.Address, &displayName, &row.Volume, &row.Trades, &row.RealizedPnl, &wins,
			&longCount, &shortCount, &lastTrade, &posSide, &posNotional,
			&posUpnl, &posLev, &marginUsed, &equity,
		); err != nil {
			return nil, 0, err
		}
		row.DisplayName = displayName
		row.LongCount = longCount
		row.ShortCount = shortCount
		row.LastTradeAt = lastTrade
		if posSide != nil {
			row.PositionSide = *posSide
		} else {
			row.PositionSide = ""
		}
		row.PositionNotional = posNotional
		row.PositionUpnl = posUpnl
		row.PositionLeverage = posLev
		row.MarginUsed = marginUsed
		if row.Trades > 0 {
			v := 100.0 * float64(wins) / float64(row.Trades)
			row.WinRate = &v
		}
		// cohort/bias
		row.Bias = Sentiment(row.LongCount, row.ShortCount)
		if equity != nil {
			row.Cohort = AccountBucketFor(*equity)
		} else {
			row.Cohort = ""
		}
		out = append(out, row)
	}
	if out == nil {
		out = []MarketWalletRow{}
	}
	return out, total, rows.Err()
}

// GetMarketTrades returns paginated market trades.
func (r *Repository) GetMarketTrades(ctx context.Context, venueID uuid.UUID, market string, from, to time.Time, side string, page, limit int) ([]MarketTradeRow, int, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	offset := (page - 1) * limit

	sideFilter := ""
	args := []any{venueID, market, from, to}
	if side != "" && strings.ToLower(side) != "all" {
		sideFilter = " AND t.side = $5"
		args = append(args, strings.ToUpper(side))
	}

	countQuery := fmt.Sprintf(`
		SELECT COUNT(*)::int FROM trader_trades t
		WHERE t.venue_id=$1 AND t.market=$2 AND t.closed_at >= $3 AND t.closed_at <= $4 %s`, sideFilter)
	var total int
	if err := r.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	dataQuery := fmt.Sprintf(`
		SELECT t.closed_at, t.wallet_address, r.display_name, t.side, t.size,
		  t.entry_price, t.exit_price, t.volume, t.pnl, t.fees, t.fills
		FROM trader_trades t
		JOIN trader_registry r ON r.venue_id=t.venue_id AND r.wallet_address=t.wallet_address
		WHERE t.venue_id=$1 AND t.market=$2 AND t.closed_at >= $3 AND t.closed_at <= $4 %s
		ORDER BY t.closed_at DESC
		LIMIT $%d OFFSET $%d`, sideFilter, len(args)+1, len(args)+2)
	args = append(args, limit, offset)
	rows, err := r.pool.Query(ctx, dataQuery, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []MarketTradeRow
	for rows.Next() {
		var row MarketTradeRow
		var size *float64
		// size may be null in old rows; default 0
		if err := rows.Scan(&row.ClosedAt, &row.WalletAddress, &row.DisplayName, &row.Side, &size,
			&row.EntryPrice, &row.ExitPrice, &row.Volume, &row.Pnl, &row.Fees, &row.Fills); err != nil {
			return nil, 0, err
		}
		if size != nil {
			row.Size = *size
		}
		out = append(out, row)
	}
	if out == nil {
		out = []MarketTradeRow{}
	}
	return out, total, rows.Err()
}

// WatchedMarkets CRUD
func (r *Repository) ListWatchedMarkets(ctx context.Context) ([]WatchedMarket, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT w.venue_id, v.code, w.market, w.enabled, w.sort_order, w.created_at, w.updated_at
		FROM watched_markets w JOIN venues v ON v.id = w.venue_id
		ORDER BY v.code, w.sort_order, w.market`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WatchedMarket
	for rows.Next() {
		var w WatchedMarket
		if err := rows.Scan(&w.VenueID, &w.Venue, &w.Market, &w.Enabled, &w.SortOrder, &w.CreatedAt, &w.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	if out == nil {
		out = []WatchedMarket{}
	}
	return out, rows.Err()
}

func (r *Repository) CreateWatchedMarket(ctx context.Context, venueID uuid.UUID, market string, enabled bool, sortOrder int) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO watched_markets (venue_id, market, enabled, sort_order)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (venue_id, market) DO UPDATE SET
		  enabled = EXCLUDED.enabled,
		  sort_order = EXCLUDED.sort_order,
		  updated_at = NOW()`, venueID, market, enabled, sortOrder)
	return err
}

func (r *Repository) UpdateWatchedMarket(ctx context.Context, venueID uuid.UUID, market string, enabled *bool, sortOrder *int) error {
	// build dynamic
	sets := []string{"updated_at = NOW()"}
	args := []any{venueID, market}
	idx := 3
	if enabled != nil {
		sets = append(sets, fmt.Sprintf("enabled = $%d", idx))
		args = append(args, *enabled)
		idx++
	}
	if sortOrder != nil {
		sets = append(sets, fmt.Sprintf("sort_order = $%d", idx))
		args = append(args, *sortOrder)
		idx++
	}
	q := fmt.Sprintf("UPDATE watched_markets SET %s WHERE venue_id=$1 AND market=$2", strings.Join(sets, ", "))
	tag, err := r.pool.Exec(ctx, q, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.NewError(domain.ErrCodeNotFound, "watched market not found")
	}
	return nil
}

func (r *Repository) DeleteWatchedMarket(ctx context.Context, venueID uuid.UUID, market string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM watched_markets WHERE venue_id=$1 AND market=$2`, venueID, market)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.NewError(domain.ErrCodeNotFound, "watched market not found")
	}
	return nil
}

func parseFloatPtrPtr(s *string) *float64 {
	if s == nil {
		return nil
	}
	v, err := strconv.ParseFloat(*s, 64)
	if err != nil {
		return nil
	}
	return &v
}
