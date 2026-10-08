package trader

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

// Repository persists the v1.1 scanner read model. All grains are keyed by
// (venue, wallet address); addresses must be pre-normalized (NormalizeAddress).
type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

// VenueIDByCode resolves a venue code (e.g. "hyperliquid") to its id.
func (r *Repository) VenueIDByCode(ctx context.Context, code string) (uuid.UUID, error) {
	var id uuid.UUID
	if err := r.pool.QueryRow(ctx,
		`SELECT id FROM venues WHERE code = $1`, code).Scan(&id); err != nil {
		if err == pgx.ErrNoRows {
			return uuid.Nil, domain.NewError(domain.ErrCodeNotFound, "unknown venue")
		}
		return uuid.Nil, err
	}
	return id, nil
}

// UpsertRegistry inserts or refreshes a registry row (spec §6, BE-001/002).
// first_seen_at is set once; last_seen_at always bumps; display_name updates
// when the incoming value is non-null; discovery_source merges to "both" when
// a second distinct non-manual source appears; "manual" never overwrites.
// last_trade_at keeps the max. Returns created=true on insert.
func (r *Repository) UpsertRegistry(ctx context.Context, venueID uuid.UUID, addr string,
	source DiscoverySource, displayName *string, tradeAt *time.Time) (*RegistryEntry, bool, error) {
	var e RegistryEntry
	var created bool
	err := r.pool.QueryRow(ctx, `
		INSERT INTO trader_registry
			(venue_id, wallet_address, discovery_source, display_name, last_trade_at, leaderboard_seen_at)
		VALUES ($1, $2, $3, $4, $5,
			CASE WHEN $3 IN ('leaderboard', 'both') THEN NOW() ELSE NULL END)
		ON CONFLICT (venue_id, wallet_address) DO UPDATE SET
			last_seen_at = NOW(),
			updated_at = NOW(),
			last_trade_at = (SELECT MAX(t) FROM (VALUES
				(trader_registry.last_trade_at), (EXCLUDED.last_trade_at)) v(t)),
			leaderboard_seen_at = CASE
				WHEN EXCLUDED.discovery_source IN ('leaderboard', 'both') THEN NOW()
				ELSE trader_registry.leaderboard_seen_at END,
			display_name = COALESCE(EXCLUDED.display_name, trader_registry.display_name),
			discovery_source = CASE
				WHEN trader_registry.discovery_source = EXCLUDED.discovery_source
					THEN trader_registry.discovery_source
				WHEN EXCLUDED.discovery_source = 'manual' THEN trader_registry.discovery_source
				WHEN trader_registry.discovery_source = 'manual' THEN EXCLUDED.discovery_source
				ELSE 'both' END
		RETURNING venue_id, wallet_address, first_seen_at, last_seen_at, last_trade_at,
			discovery_source, leaderboard_seen_at, display_name, status, (xmax = 0)`,
		venueID, addr, string(source), displayName, tradeAt,
	).Scan(&e.VenueID, &e.WalletAddress, &e.FirstSeenAt, &e.LastSeenAt, &e.LastTradeAt,
		&e.DiscoverySource, &e.LeaderboardSeenAt, &e.DisplayName, &e.Status, &created)
	if err != nil {
		return nil, false, err
	}
	return &e, created, nil
}

// GetRegistry loads one registry row with its venue code.
func (r *Repository) GetRegistry(ctx context.Context, venueID uuid.UUID, addr string) (*RegistryEntry, error) {
	var e RegistryEntry
	err := r.pool.QueryRow(ctx, `
		SELECT r.venue_id, v.code, r.wallet_address, r.first_seen_at, r.last_seen_at,
			r.last_trade_at, r.discovery_source, r.leaderboard_seen_at, r.display_name, r.status
		FROM trader_registry r JOIN venues v ON v.id = r.venue_id
		WHERE r.venue_id = $1 AND r.wallet_address = $2`,
		venueID, addr,
	).Scan(&e.VenueID, &e.Venue, &e.WalletAddress, &e.FirstSeenAt, &e.LastSeenAt,
		&e.LastTradeAt, &e.DiscoverySource, &e.LeaderboardSeenAt, &e.DisplayName, &e.Status)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, domain.NewError(domain.ErrCodeNotFound, "trader not found")
		}
		return nil, err
	}
	return &e, nil
}

// EnsureSyncState creates a pending sync row if absent, then returns it.
func (r *Repository) EnsureSyncState(ctx context.Context, venueID uuid.UUID, addr string) (*SyncState, error) {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO trader_sync_state (venue_id, wallet_address)
		VALUES ($1, $2) ON CONFLICT (venue_id, wallet_address) DO NOTHING`,
		venueID, addr)
	if err != nil {
		return nil, err
	}
	return r.GetSyncState(ctx, venueID, addr)
}

// GetSyncState loads the sync row (nil, nil when never initialized).
func (r *Repository) GetSyncState(ctx context.Context, venueID uuid.UUID, addr string) (*SyncState, error) {
	var s SyncState
	err := r.pool.QueryRow(ctx, `
		SELECT venue_id, wallet_address, fills_last_time, fills_last_tid,
			last_fills_sync_at, last_portfolio_sync_at, last_positions_sync_at, backfill_start_time,
			backfill_completed_at, sync_status, retry_count, last_error, updated_at
		FROM trader_sync_state WHERE venue_id = $1 AND wallet_address = $2`,
		venueID, addr,
	).Scan(&s.VenueID, &s.WalletAddress, &s.FillsLastTime, &s.FillsLastTID,
		&s.LastFillsSyncAt, &s.LastPortfolioSyncAt, &s.LastPositionsSyncAt, &s.BackfillStartTime,
		&s.BackfillCompletedAt, &s.SyncStatus, &s.RetryCount, &s.LastError, &s.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &s, nil
}

// UpdateSyncProgress advances cursors/status. Empty status keeps the current
// one; non-nil lastErr sets (empty string clears); retryDelta adds to the
// retry counter (use negative to reset — caller clamps at zero via GREATEST).
func (r *Repository) UpdateSyncProgress(ctx context.Context, venueID uuid.UUID, addr string,
	fillsLastTime *time.Time, fillsLastTID *int64, status string, lastErr *string, retryDelta int) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE trader_sync_state SET
			fills_last_time = COALESCE($3::timestamptz, fills_last_time),
			fills_last_tid = COALESCE($4::bigint, fills_last_tid),
			sync_status = CASE WHEN $5 = '' THEN sync_status ELSE $5 END,
			last_error = CASE WHEN $6::text IS NULL THEN last_error ELSE NULLIF($6::text, '') END,
			retry_count = GREATEST(0, retry_count + $7),
			updated_at = NOW()
		WHERE venue_id = $1 AND wallet_address = $2`,
		venueID, addr, fillsLastTime, fillsLastTID, status, lastErr, retryDelta)
	return err
}

// UpsertDailyStats replaces the whole day row (idempotent recompute, BE-020:
// same input twice yields an identical row).
func (r *Repository) UpsertDailyStats(ctx context.Context, s *DailyStats) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO trader_daily_stats
			(venue_id, wallet_address, stat_date, trade_count, win_count, loss_count,
			 breakeven_count, realized_pnl, fees, volume, gross_profit, gross_loss,
			 long_count, long_wins, short_count, short_wins,
			 holding_time_sec_sum, holding_time_sec_count, last_trade_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)
		ON CONFLICT (venue_id, wallet_address, stat_date) DO UPDATE SET
			trade_count = EXCLUDED.trade_count, win_count = EXCLUDED.win_count,
			loss_count = EXCLUDED.loss_count, breakeven_count = EXCLUDED.breakeven_count,
			realized_pnl = EXCLUDED.realized_pnl, fees = EXCLUDED.fees,
			volume = EXCLUDED.volume, gross_profit = EXCLUDED.gross_profit,
			gross_loss = EXCLUDED.gross_loss, long_count = EXCLUDED.long_count,
			long_wins = EXCLUDED.long_wins, short_count = EXCLUDED.short_count,
			short_wins = EXCLUDED.short_wins,
			holding_time_sec_sum = EXCLUDED.holding_time_sec_sum,
			holding_time_sec_count = EXCLUDED.holding_time_sec_count,
			last_trade_at = EXCLUDED.last_trade_at,
			updated_at = NOW()`,
		s.VenueID, s.WalletAddress, s.StatDate, s.TradeCount, s.WinCount, s.LossCount,
		s.BreakevenCount, s.RealizedPnL, s.Fees, s.Volume, s.GrossProfit, s.GrossLoss,
		s.LongCount, s.LongWins, s.ShortCount, s.ShortWins,
		s.HoldingTimeSecSum, s.HoldingTimeSecCount, s.LastTradeAt)
	return err
}

// GetDailyStats loads one day row (nil, nil when absent).
func (r *Repository) GetDailyStats(ctx context.Context, venueID uuid.UUID, addr string, day time.Time) (*DailyStats, error) {
	var s DailyStats
	err := r.pool.QueryRow(ctx, `
		SELECT venue_id, wallet_address, stat_date, trade_count, win_count, loss_count,
			breakeven_count, realized_pnl, fees, volume, gross_profit, gross_loss,
			long_count, long_wins, short_count, short_wins,
			holding_time_sec_sum, holding_time_sec_count, last_trade_at
		FROM trader_daily_stats
		WHERE venue_id = $1 AND wallet_address = $2 AND stat_date = $3`,
		venueID, addr, day.Format("2006-01-02"),
	).Scan(&s.VenueID, &s.WalletAddress, &s.StatDate, &s.TradeCount, &s.WinCount,
		&s.LossCount, &s.BreakevenCount, &s.RealizedPnL, &s.Fees, &s.Volume,
		&s.GrossProfit, &s.GrossLoss, &s.LongCount, &s.LongWins, &s.ShortCount,
		&s.ShortWins, &s.HoldingTimeSecSum, &s.HoldingTimeSecCount, &s.LastTradeAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &s, nil
}

// UpsertPeriodMetrics replaces the whole period row (recalc after daily
// aggregates commit, spec §11).
func (r *Repository) UpsertPeriodMetrics(ctx context.Context, m *PeriodMetrics) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO trader_period_metrics
			(venue_id, wallet_address, period, as_of, pnl, realized_pnl, roi, win_rate, trade_count,
			 volume, gross_profit, gross_loss, profit_factor, avg_trade_pnl,
			 long_count, long_wins, short_count, short_wins, max_drawdown_pct,
			 avg_holding_time_sec, last_trade_at, data_status, is_partial, calculation_version)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24)
		ON CONFLICT (venue_id, wallet_address, period) DO UPDATE SET
			as_of = EXCLUDED.as_of, pnl = EXCLUDED.pnl, realized_pnl = EXCLUDED.realized_pnl,
			roi = EXCLUDED.roi,
			win_rate = EXCLUDED.win_rate, trade_count = EXCLUDED.trade_count,
			volume = EXCLUDED.volume, gross_profit = EXCLUDED.gross_profit,
			gross_loss = EXCLUDED.gross_loss, profit_factor = EXCLUDED.profit_factor,
			avg_trade_pnl = EXCLUDED.avg_trade_pnl, long_count = EXCLUDED.long_count,
			long_wins = EXCLUDED.long_wins, short_count = EXCLUDED.short_count,
			short_wins = EXCLUDED.short_wins, max_drawdown_pct = EXCLUDED.max_drawdown_pct,
			avg_holding_time_sec = EXCLUDED.avg_holding_time_sec,
			last_trade_at = EXCLUDED.last_trade_at, data_status = EXCLUDED.data_status,
			is_partial = EXCLUDED.is_partial,
			calculation_version = EXCLUDED.calculation_version, updated_at = NOW()`,
		m.VenueID, m.WalletAddress, m.Period, m.AsOf, m.PnL, m.RealizedPnL, m.ROI, m.WinRate,
		m.TradeCount, m.Volume, m.GrossProfit, m.GrossLoss, m.ProfitFactor,
		m.AvgTradePnL, m.LongCount, m.LongWins, m.ShortCount, m.ShortWins,
		m.MaxDrawdownPct, m.AvgHoldingTimeSec, m.LastTradeAt, string(m.DataStatus),
		m.IsPartial, m.CalculationVersion)
	return err
}

// GetPeriodMetrics loads one period row with venue code (nil, nil when absent).
func (r *Repository) GetPeriodMetrics(ctx context.Context, venueID uuid.UUID, addr, period string) (*PeriodMetrics, error) {
	var m PeriodMetrics
	var status string
	err := r.pool.QueryRow(ctx, `
		SELECT p.venue_id, v.code, p.wallet_address, p.period, p.as_of, p.pnl, p.realized_pnl, p.roi,
			p.win_rate, p.trade_count, p.volume, p.gross_profit, p.gross_loss,
			p.profit_factor, p.avg_trade_pnl, p.long_count, p.long_wins,
			p.short_count, p.short_wins, p.max_drawdown_pct, p.avg_holding_time_sec,
			p.last_trade_at, p.data_status, p.is_partial, p.calculation_version
		FROM trader_period_metrics p JOIN venues v ON v.id = p.venue_id
		WHERE p.venue_id = $1 AND p.wallet_address = $2 AND p.period = $3`,
		venueID, addr, period,
	).Scan(&m.VenueID, &m.Venue, &m.WalletAddress, &m.Period, &m.AsOf, &m.PnL, &m.RealizedPnL,
		&m.ROI, &m.WinRate, &m.TradeCount, &m.Volume, &m.GrossProfit, &m.GrossLoss,
		&m.ProfitFactor, &m.AvgTradePnL, &m.LongCount, &m.LongWins, &m.ShortCount,
		&m.ShortWins, &m.MaxDrawdownPct, &m.AvgHoldingTimeSec, &m.LastTradeAt,
		&status, &m.IsPartial, &m.CalculationVersion)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	m.DataStatus = DataStatus(status)
	return &m, nil
}

// UpsertLeaderboardRef stores the latest upstream window aggregates (3 numbers
// per window; raw payloads never stored). ROI passthrough source (spec D4).
func (r *Repository) UpsertLeaderboardRef(ctx context.Context, ref *LeaderboardRef) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO trader_leaderboard_ref
			(venue_id, wallet_address, lb_window, pnl, roi, volume, account_value, fetched_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (venue_id, wallet_address, lb_window) DO UPDATE SET
			pnl = EXCLUDED.pnl, roi = EXCLUDED.roi, volume = EXCLUDED.volume,
			account_value = EXCLUDED.account_value, fetched_at = EXCLUDED.fetched_at`,
		ref.VenueID, ref.WalletAddress, ref.Window, ref.PnL, ref.ROI,
		ref.Volume, ref.AccountValue, ref.FetchedAt)
	return err
}

// GetLeaderboardRef loads one window reference (nil, nil when absent).
func (r *Repository) GetLeaderboardRef(ctx context.Context, venueID uuid.UUID, addr, window string) (*LeaderboardRef, error) {
	var ref LeaderboardRef
	err := r.pool.QueryRow(ctx, `
		SELECT venue_id, wallet_address, lb_window, pnl, roi, volume, account_value, fetched_at
		FROM trader_leaderboard_ref
		WHERE venue_id = $1 AND wallet_address = $2 AND lb_window = $3`,
		venueID, addr, window,
	).Scan(&ref.VenueID, &ref.WalletAddress, &ref.Window, &ref.PnL, &ref.ROI,
		&ref.Volume, &ref.AccountValue, &ref.FetchedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &ref, nil
}

// ListDailyStats returns day rows in [from, to] (inclusive dates) oldest-first
// for period aggregation.
func (r *Repository) ListDailyStats(ctx context.Context, venueID uuid.UUID, addr string, from, to time.Time) ([]*DailyStats, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT venue_id, wallet_address, stat_date, trade_count, win_count, loss_count,
			breakeven_count, realized_pnl, fees, volume, gross_profit, gross_loss,
			long_count, long_wins, short_count, short_wins,
			holding_time_sec_sum, holding_time_sec_count, last_trade_at
		FROM trader_daily_stats
		WHERE venue_id = $1 AND wallet_address = $2
		  AND stat_date >= $3 AND stat_date <= $4
		ORDER BY stat_date ASC`,
		venueID, addr, from.Format("2006-01-02"), to.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*DailyStats
	for rows.Next() {
		var s DailyStats
		if err := rows.Scan(&s.VenueID, &s.WalletAddress, &s.StatDate, &s.TradeCount,
			&s.WinCount, &s.LossCount, &s.BreakevenCount, &s.RealizedPnL, &s.Fees,
			&s.Volume, &s.GrossProfit, &s.GrossLoss, &s.LongCount, &s.LongWins,
			&s.ShortCount, &s.ShortWins, &s.HoldingTimeSecSum, &s.HoldingTimeSecCount,
			&s.LastTradeAt); err != nil {
			return nil, err
		}
		out = append(out, &s)
	}
	return out, rows.Err()
}

// UpsertRegistryWS bulk-upserts WS-harvested addresses in ONE statement (no
// per-event transactions, spec §6/§11): insert source=ws_trade, refresh
// last_seen_at, keep max last_trade_at, merge leaderboard→both, manual stays.
// Returns touched row count.
func (r *Repository) UpsertRegistryWS(ctx context.Context, venueID uuid.UUID, addrs []string, times []time.Time) (int64, error) {
	if len(addrs) == 0 {
		return 0, nil
	}
	tag, err := r.pool.Exec(ctx, `
		INSERT INTO trader_registry
			(venue_id, wallet_address, discovery_source, last_trade_at, last_seen_at)
		SELECT $1, addr, 'ws_trade', t, NOW()
		FROM UNNEST($2::text[], $3::timestamptz[]) AS v(addr, t)
		ON CONFLICT (venue_id, wallet_address) DO UPDATE SET
			last_seen_at = NOW(),
			updated_at = NOW(),
			last_trade_at = (SELECT MAX(t) FROM (VALUES
				(trader_registry.last_trade_at), (EXCLUDED.last_trade_at)) v(t)),
			discovery_source = CASE
				WHEN trader_registry.discovery_source = 'leaderboard' THEN 'both'
				ELSE trader_registry.discovery_source END`,
		venueID, addrs, times)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// SyncItem is one schedulable wallet: identity plus the tier/backoff inputs.
type SyncItem struct {
	Address     string
	LastTradeAt *time.Time
	LastSuccess *time.Time
	Status      string
	UpdatedAt   time.Time
	RetryCount  int
	HasSyncRow  bool
}

// ListSyncQueue returns active registry wallets with scheduling inputs,
// pending first (detail views EnsureSyncState, so on-demand interest is
// picked up earlier — spec BE-038).
func (r *Repository) ListSyncQueue(ctx context.Context, venueID uuid.UUID) ([]SyncItem, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT r.wallet_address, r.last_trade_at, s.last_fills_sync_at,
			COALESCE(s.sync_status, 'pending'), COALESCE(s.updated_at, r.first_seen_at),
			COALESCE(s.retry_count, 0), s.wallet_address IS NOT NULL
		FROM trader_registry r
		LEFT JOIN trader_sync_state s ON s.venue_id = r.venue_id
			AND s.wallet_address = r.wallet_address
		WHERE r.venue_id = $1 AND r.status = 'active'
		ORDER BY CASE WHEN s.sync_status IS NULL OR s.sync_status = 'pending'
			THEN 0 ELSE 1 END, s.updated_at ASC NULLS FIRST, r.wallet_address ASC`, venueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SyncItem
	for rows.Next() {
		var it SyncItem
		if err := rows.Scan(&it.Address, &it.LastTradeAt, &it.LastSuccess,
			&it.Status, &it.UpdatedAt, &it.RetryCount, &it.HasSyncRow); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// ReplacePositions replaces the whole open-position snapshot for one wallet
// in ONE transaction (DETAIL-PLAN A4): UPSERT the margin summary, UPSERT
// each position coin, DELETE coins absent from the snapshot. Empty snapshots
// still persist the summary and delete all coins (flat account).
func (r *Repository) ReplacePositions(ctx context.Context, venueID uuid.UUID, addr string, snap *PositionSnapshot) error {
	if snap == nil {
		return domain.NewError(domain.ErrCodeValidation, "nil position snapshot")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	asOf := snap.AsOf.UTC()
	if asOf.IsZero() {
		asOf = time.Now().UTC()
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO trader_position_summary
			(venue_id, wallet_address, account_value, total_ntl_pos,
			 total_margin_used, as_of, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,NOW())
		ON CONFLICT (venue_id, wallet_address) DO UPDATE SET
			account_value = EXCLUDED.account_value,
			total_ntl_pos = EXCLUDED.total_ntl_pos,
			total_margin_used = EXCLUDED.total_margin_used,
			as_of = EXCLUDED.as_of, updated_at = NOW()`,
		venueID, addr, snap.AccountValue, snap.TotalNtlPos,
		snap.TotalMarginUsed, asOf); err != nil {
		return err
	}
	coins := make([]string, 0, len(snap.Positions))
	for _, p := range snap.Positions {
		coin := strings.ToUpper(strings.TrimSpace(p.Coin))
		if coin == "" {
			return domain.NewError(domain.ErrCodeValidation, "position coin is required")
		}
		if p.Side != "LONG" && p.Side != "SHORT" {
			return domain.NewError(domain.ErrCodeValidation, "invalid position side "+p.Side+": want LONG|SHORT")
		}
		coins = append(coins, coin)
		side := p.Side
		if _, err := tx.Exec(ctx, `
			INSERT INTO trader_positions
				(venue_id, wallet_address, coin, side, size, entry_price,
				 mark_price, position_value, unrealized_pnl, return_on_equity,
				 liquidation_price, leverage, max_leverage, margin_used,
				 as_of, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,NOW())
			ON CONFLICT (venue_id, wallet_address, coin) DO UPDATE SET
				side = EXCLUDED.side, size = EXCLUDED.size,
				entry_price = EXCLUDED.entry_price, mark_price = EXCLUDED.mark_price,
				position_value = EXCLUDED.position_value,
				unrealized_pnl = EXCLUDED.unrealized_pnl,
				return_on_equity = EXCLUDED.return_on_equity,
				liquidation_price = EXCLUDED.liquidation_price,
				leverage = EXCLUDED.leverage, max_leverage = EXCLUDED.max_leverage,
				margin_used = EXCLUDED.margin_used, as_of = EXCLUDED.as_of,
				updated_at = NOW()`,
			venueID, addr, coin, side, p.Size, p.EntryPrice,
			p.MarkPrice, p.PositionValue, p.UnrealizedPnl, p.ReturnOnEquity,
			p.LiquidationPrice, p.Leverage, p.MaxLeverage, p.MarginUsed,
			asOf); err != nil {
			return err
		}
	}
	if len(coins) == 0 {
		if _, err := tx.Exec(ctx, `
			DELETE FROM trader_positions
			WHERE venue_id = $1 AND wallet_address = $2`,
			venueID, addr); err != nil {
			return err
		}
	} else {
		if _, err := tx.Exec(ctx, `
			DELETE FROM trader_positions
			WHERE venue_id = $1 AND wallet_address = $2
			  AND coin <> ALL($3)`,
			venueID, addr, coins); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// GetPositions returns the latest snapshot coins oldest-coin-first
// (deterministic for the REST DTO). Empty (never empty-nil) when none.
func (r *Repository) GetPositions(ctx context.Context, venueID uuid.UUID, addr string) ([]Position, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT coin, side, size, entry_price, mark_price, position_value,
			unrealized_pnl, return_on_equity, liquidation_price, leverage,
			max_leverage, margin_used
		FROM trader_positions
		WHERE venue_id = $1 AND wallet_address = $2
		ORDER BY coin ASC`,
		venueID, addr)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Position{}
	for rows.Next() {
		var p Position
		if err := rows.Scan(&p.Coin, &p.Side, &p.Size, &p.EntryPrice,
			&p.MarkPrice, &p.PositionValue, &p.UnrealizedPnl, &p.ReturnOnEquity,
			&p.LiquidationPrice, &p.Leverage, &p.MaxLeverage, &p.MarginUsed); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetPositionSummary loads the account margin summary (nil, nil when never
// synced).
func (r *Repository) GetPositionSummary(ctx context.Context, venueID uuid.UUID, addr string) (*PositionSummary, error) {
	var s PositionSummary
	err := r.pool.QueryRow(ctx, `
		SELECT account_value, total_ntl_pos, total_margin_used, as_of
		FROM trader_position_summary
		WHERE venue_id = $1 AND wallet_address = $2`,
		venueID, addr,
	).Scan(&s.AccountValue, &s.TotalNtlPos, &s.TotalMarginUsed, &s.AsOf)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &s, nil
}

// ReplaceTradesForDay replaces all durable closed trades whose close time
// falls on day (UTC) in ONE transaction: DELETE the day window, then INSERT
// the recomputed list. Empty lists still delete (a recompute that closed the
// cycle must clear stale rows). Crash-safe recompute (DETAIL-PLAN A4).
// Entry/exit/size (migration 000030) are written when present; pre-000030
// rows keep NULL after the backfill window ages out.
func (r *Repository) ReplaceTradesForDay(ctx context.Context, venueID uuid.UUID, addr string, day time.Time, trades []CompletedTrade) error {
	day = day.UTC().Truncate(24 * time.Hour)
	next := day.Add(24 * time.Hour)
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
		DELETE FROM trader_trades
		WHERE venue_id = $1 AND wallet_address = $2
		  AND closed_at >= $3 AND closed_at < $4`,
		venueID, addr, day, next); err != nil {
		return err
	}
	for _, t := range trades {
		side := "SHORT"
		if t.Long {
			side = "LONG"
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO trader_trades
				(venue_id, wallet_address, market, side, opened_at, closed_at,
				 volume, pnl, fees, fills, entry_price, exit_price, size, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,NOW())
			ON CONFLICT (venue_id, wallet_address, market, opened_at, closed_at)
			DO UPDATE SET volume = EXCLUDED.volume, pnl = EXCLUDED.pnl,
				fees = EXCLUDED.fees, fills = EXCLUDED.fills,
				side = EXCLUDED.side,
				entry_price = EXCLUDED.entry_price, exit_price = EXCLUDED.exit_price,
				size = EXCLUDED.size, updated_at = NOW()`,
			venueID, addr, t.Market, side, t.OpenTime.UTC(), t.CloseTime.UTC(),
			t.Volume, t.PnL, t.Fees, t.Fills, t.EntryPrice, t.ExitPrice, t.Size); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// ListTrades returns closed trades newest-first with keyset pagination on
// (closed_at DESC, market ASC, opened_at ASC). Pass afterClosedAt == nil for
// the first page; otherwise the cursor triple from the previous page's last
// row. The caller fetches limit+1 to detect has_more (DETAIL-PLAN A4).
// Entry/exit/size added by migration 000030 (NULL for old rows).
func (r *Repository) ListTrades(ctx context.Context, venueID uuid.UUID, addr string, limit int, afterClosedAt *time.Time, afterMarket string, afterOpenedAt *time.Time) ([]CompletedTradeRow, error) {
	if limit <= 0 {
		limit = 20
	}
	var afterOpened time.Time
	if afterOpenedAt != nil {
		afterOpened = afterOpenedAt.UTC()
	}
	rows, err := r.pool.Query(ctx, `
		SELECT market, side, opened_at, closed_at, volume, pnl, fees, fills,
			entry_price, exit_price, size
		FROM trader_trades
		WHERE venue_id = $1 AND wallet_address = $2
		  AND ($3::timestamptz IS NULL
			OR closed_at < $3
			OR (closed_at = $3 AND market > $4)
			OR (closed_at = $3 AND market = $4 AND opened_at > $5))
		ORDER BY closed_at DESC, market ASC, opened_at ASC
		LIMIT $6`,
		venueID, addr, afterClosedAt, afterMarket, afterOpened, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CompletedTradeRow{}
	for rows.Next() {
		var row CompletedTradeRow
		if err := rows.Scan(&row.Market, &row.Side, &row.OpenedAt, &row.ClosedAt,
			&row.Volume, &row.PnL, &row.Fees, &row.Fills,
			&row.EntryPrice, &row.ExitPrice, &row.Size); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// ListAllTrades returns every retained closed trade for the wallet (15-day
// window) with entry/exit/size. The service sorts/filters/paginates in memory
// so any §1.2 sort key gets a stable keyset cursor without 18 SQL variants.
// Bounded by retention, so the full scan stays small (hundreds of rows).
func (r *Repository) ListAllTrades(ctx context.Context, venueID uuid.UUID, addr string) ([]CompletedTradeRow, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT market, side, opened_at, closed_at, volume, pnl, fees, fills,
			entry_price, exit_price, size
		FROM trader_trades
		WHERE venue_id = $1 AND wallet_address = $2`,
		venueID, addr)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CompletedTradeRow{}
	for rows.Next() {
		var row CompletedTradeRow
		if err := rows.Scan(&row.Market, &row.Side, &row.OpenedAt, &row.ClosedAt,
			&row.Volume, &row.PnL, &row.Fees, &row.Fills,
			&row.EntryPrice, &row.ExitPrice, &row.Size); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// PruneTrades deletes durable closed trades with closed_at before cutoff
// (15-day retention, DETAIL-PLAN D5). Returns deleted rows.
func (r *Repository) PruneTrades(ctx context.Context, venueID uuid.UUID, addr string, before time.Time) (int64, error) {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM trader_trades
		WHERE venue_id = $1 AND wallet_address = $2 AND closed_at < $3`,
		venueID, addr, before.UTC())
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// countSearch executes a COUNT(*) built by buildSearchCountQuery over the
// identical filters as the data query (CONTRACT.md B2).
func (r *Repository) countSearch(ctx context.Context, query string, args []any) (int64, error) {
	var total int64
	if err := r.pool.QueryRow(ctx, query, args...).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

// searchRaw executes a query built by buildSearchQuery and scans period rows
// with venue code + registry display name.
func (r *Repository) searchRaw(ctx context.Context, query string, args []any) ([]*PeriodMetrics, error) {
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*PeriodMetrics
	for rows.Next() {
		var m PeriodMetrics
		var status string
		if err := rows.Scan(&m.VenueID, &m.Venue, &m.WalletAddress, &m.DisplayName,
			&m.Period, &m.AsOf, &m.PnL, &m.RealizedPnL, &m.ROI, &m.WinRate, &m.TradeCount, &m.Volume,
			&m.GrossProfit, &m.GrossLoss, &m.ProfitFactor, &m.AvgTradePnL,
			&m.LongCount, &m.LongWins, &m.ShortCount, &m.ShortWins, &m.MaxDrawdownPct,
			&m.AvgHoldingTimeSec, &m.LastTradeAt, &status, &m.IsPartial,
			&m.CalculationVersion); err != nil {
			return nil, err
		}
		m.DataStatus = DataStatus(status)
		out = append(out, &m)
	}
	return out, rows.Err()
}
