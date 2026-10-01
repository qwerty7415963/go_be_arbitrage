package trader

import (
	"context"
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
			last_fills_sync_at, last_portfolio_sync_at, backfill_start_time,
			backfill_completed_at, sync_status, retry_count, last_error, updated_at
		FROM trader_sync_state WHERE venue_id = $1 AND wallet_address = $2`,
		venueID, addr,
	).Scan(&s.VenueID, &s.WalletAddress, &s.FillsLastTime, &s.FillsLastTID,
		&s.LastFillsSyncAt, &s.LastPortfolioSyncAt, &s.BackfillStartTime,
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
			(venue_id, wallet_address, period, as_of, pnl, roi, win_rate, trade_count,
			 volume, gross_profit, gross_loss, profit_factor, avg_trade_pnl,
			 long_count, long_wins, short_count, short_wins, max_drawdown_pct,
			 avg_holding_time_sec, last_trade_at, data_status, is_partial, calculation_version)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23)
		ON CONFLICT (venue_id, wallet_address, period) DO UPDATE SET
			as_of = EXCLUDED.as_of, pnl = EXCLUDED.pnl, roi = EXCLUDED.roi,
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
		m.VenueID, m.WalletAddress, m.Period, m.AsOf, m.PnL, m.ROI, m.WinRate,
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
		SELECT p.venue_id, v.code, p.wallet_address, p.period, p.as_of, p.pnl, p.roi,
			p.win_rate, p.trade_count, p.volume, p.gross_profit, p.gross_loss,
			p.profit_factor, p.avg_trade_pnl, p.long_count, p.long_wins,
			p.short_count, p.short_wins, p.max_drawdown_pct, p.avg_holding_time_sec,
			p.last_trade_at, p.data_status, p.is_partial, p.calculation_version
		FROM trader_period_metrics p JOIN venues v ON v.id = p.venue_id
		WHERE p.venue_id = $1 AND p.wallet_address = $2 AND p.period = $3`,
		venueID, addr, period,
	).Scan(&m.VenueID, &m.Venue, &m.WalletAddress, &m.Period, &m.AsOf, &m.PnL,
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

// ListRegistryAddresses returns active registry addresses for a venue (sync
// scheduling input).
func (r *Repository) ListRegistryAddresses(ctx context.Context, venueID uuid.UUID) ([]string, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT wallet_address FROM trader_registry
		WHERE venue_id = $1 AND status = 'active' ORDER BY wallet_address ASC`, venueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
