package tradergroup

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
	"github.com/qwerty7415963/go_be_arbitrage/internal/trader"
)

// Repository persists trader_groups + trader_group_members. Every read/write
// is owner-scoped: unknown group is 404, foreign group is 403 (BE-028).
type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func notFound() error {
	return domain.NewError(domain.ErrCodeNotFound, "group not found")
}

func forbidden() error {
	return domain.NewError(domain.ErrCodeGroupForbidden, "not your group")
}

// requireOwner resolves the group's owner: 404 unknown, 403 foreign.
func (r *Repository) requireOwner(ctx context.Context, groupID, userID uuid.UUID) error {
	var owner uuid.UUID
	err := r.pool.QueryRow(ctx,
		`SELECT user_id FROM trader_groups WHERE id = $1`, groupID).Scan(&owner)
	if err != nil {
		if err == pgx.ErrNoRows {
			return notFound()
		}
		return err
	}
	if owner != userID {
		return forbidden()
	}
	return nil
}

// Create inserts a group; duplicate name per user is GROUP-002 (409).
func (r *Repository) Create(ctx context.Context, userID uuid.UUID, name, description string) (*Group, error) {
	var g Group
	err := r.pool.QueryRow(ctx, `
		INSERT INTO trader_groups (user_id, name, description)
		VALUES ($1, $2, $3) RETURNING id, user_id, name, description`,
		userID, name, description).Scan(&g.ID, &g.UserID, &g.Name, &g.Description)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, domain.NewError(domain.ErrCodeGroupDuplicate, "group name already exists")
		}
		return nil, err
	}
	return &g, nil
}

// List returns the caller's groups with member counts.
func (r *Repository) List(ctx context.Context, userID uuid.UUID) ([]*Group, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT g.id, g.user_id, g.name, g.description, COUNT(m.wallet_address)::int
		FROM trader_groups g LEFT JOIN trader_group_members m ON m.group_id = g.id
		WHERE g.user_id = $1 GROUP BY g.id ORDER BY g.name ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Group{}
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.ID, &g.UserID, &g.Name, &g.Description, &g.MemberCount); err != nil {
			return nil, err
		}
		out = append(out, &g)
	}
	return out, rows.Err()
}

// Get returns one owned group with its member count.
func (r *Repository) Get(ctx context.Context, groupID, userID uuid.UUID) (*Group, error) {
	if err := r.requireOwner(ctx, groupID, userID); err != nil {
		return nil, err
	}
	var g Group
	err := r.pool.QueryRow(ctx, `
		SELECT g.id, g.user_id, g.name, g.description, COUNT(m.wallet_address)::int
		FROM trader_groups g LEFT JOIN trader_group_members m ON m.group_id = g.id
		WHERE g.id = $1 GROUP BY g.id`, groupID,
	).Scan(&g.ID, &g.UserID, &g.Name, &g.Description, &g.MemberCount)
	if err != nil {
		return nil, err
	}
	return &g, nil
}

// Update renames/edits an owned group; rename collision is GROUP-002.
func (r *Repository) Update(ctx context.Context, groupID, userID uuid.UUID, name, description *string) (*Group, error) {
	if err := r.requireOwner(ctx, groupID, userID); err != nil {
		return nil, err
	}
	var g Group
	err := r.pool.QueryRow(ctx, `
		UPDATE trader_groups SET
			name = COALESCE($3, name),
			description = COALESCE($4, description),
			updated_at = NOW()
		WHERE id = $1 AND user_id = $2
		RETURNING id, user_id, name, description`,
		groupID, userID, name, description).Scan(&g.ID, &g.UserID, &g.Name, &g.Description)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, domain.NewError(domain.ErrCodeGroupDuplicate, "group name already exists")
		}
		return nil, err
	}
	return &g, nil
}

// Delete removes an owned group; memberships cascade, registry rows stay (BE-030).
func (r *Repository) Delete(ctx context.Context, groupID, userID uuid.UUID) error {
	if err := r.requireOwner(ctx, groupID, userID); err != nil {
		return err
	}
	_, err := r.pool.Exec(ctx, `DELETE FROM trader_groups WHERE id = $1`, groupID)
	return err
}

// AddMembers inserts (venue, address) memberships idempotently: existing rows
// keep their alias/note, new rows take the given ones. Returns added count.
// Unknown venues abort the whole batch (all-or-nothing transaction).
func (r *Repository) AddMembers(ctx context.Context, groupID, userID uuid.UUID, items []MemberInput) (int64, error) {
	if err := r.requireOwner(ctx, groupID, userID); err != nil {
		return 0, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var added int64
	for _, it := range items {
		var venueID uuid.UUID
		if err := tx.QueryRow(ctx,
			`SELECT id FROM venues WHERE code = $1`, it.Venue).Scan(&venueID); err != nil {
			if err == pgx.ErrNoRows {
				return 0, domain.NewError(domain.ErrCodeValidation, "unknown venue: "+it.Venue)
			}
			return 0, err
		}
		// Registry row must exist (discovery owns wallet identity).
		var exists bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS(SELECT 1 FROM trader_registry WHERE venue_id = $1 AND wallet_address = $2)`,
			venueID, it.WalletAddress).Scan(&exists); err != nil {
			return 0, err
		}
		if !exists {
			return 0, domain.NewError(domain.ErrCodeNotFound, "unknown wallet: "+it.WalletAddress)
		}
		tag, err := tx.Exec(ctx, `
			INSERT INTO trader_group_members (group_id, venue_id, wallet_address, alias, note)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (group_id, venue_id, wallet_address) DO NOTHING`,
			groupID, venueID, it.WalletAddress, it.Alias, it.Note)
		if err != nil {
			return 0, err
		}
		added += tag.RowsAffected()
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return added, nil
}

// RemoveMembers deletes memberships; absent rows are a no-op. Returns removed.
func (r *Repository) RemoveMembers(ctx context.Context, groupID, userID uuid.UUID, venueID uuid.UUID, addrs []string) (int64, error) {
	if err := r.requireOwner(ctx, groupID, userID); err != nil {
		return 0, err
	}
	if len(addrs) == 0 {
		return 0, nil
	}
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM trader_group_members
		WHERE group_id = $1 AND venue_id = $2 AND wallet_address = ANY($3)`,
		groupID, venueID, addrs)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// UpdateMembers sets alias/note on existing memberships (BE-2). Each item
// carries set-flags: unset keeps, set with empty clears to NULL, set with
// value stores it. Absent memberships are a no-op, mirroring RemoveMembers.
// Unknown venues/registry wallets fail the batch (all-or-nothing). Returns
// the number of rows actually changed.
func (r *Repository) UpdateMembers(ctx context.Context, groupID, userID uuid.UUID, items []ValidatedMemberUpdate) (int64, error) {
	if err := r.requireOwner(ctx, groupID, userID); err != nil {
		return 0, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var updated int64
	for _, it := range items {
		var venueID uuid.UUID
		if err := tx.QueryRow(ctx,
			`SELECT id FROM venues WHERE code = $1`, it.Venue).Scan(&venueID); err != nil {
			if err == pgx.ErrNoRows {
				return 0, domain.NewError(domain.ErrCodeValidation, "unknown venue: "+it.Venue)
			}
			return 0, err
		}
		var exists bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS(SELECT 1 FROM trader_registry WHERE venue_id = $1 AND wallet_address = $2)`,
			venueID, it.WalletAddress).Scan(&exists); err != nil {
			return 0, err
		}
		if !exists {
			return 0, domain.NewError(domain.ErrCodeNotFound, "unknown wallet: "+it.WalletAddress)
		}
		tag, err := tx.Exec(ctx, `
			UPDATE trader_group_members
			SET alias = CASE WHEN $4 THEN NULLIF($5, '') ELSE alias END,
			    note = CASE WHEN $6 THEN NULLIF($7, '') ELSE note END,
			    updated_at = NOW()
			WHERE group_id = $1 AND venue_id = $2 AND wallet_address = $3
			  AND (CASE WHEN $4 THEN NULLIF($5, '') ELSE alias END IS DISTINCT FROM alias
			    OR CASE WHEN $6 THEN NULLIF($7, '') ELSE note END IS DISTINCT FROM note)`,
			groupID, venueID, it.WalletAddress,
			it.SetAlias, it.Alias, it.SetNote, it.Note)
		if err != nil {
			return 0, err
		}
		updated += tag.RowsAffected()
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return updated, nil
}

// periodRow mirrors trader.PeriodMetrics with nullable holders so a missing
// period row (all NULLs from the LEFT JOIN) maps to Metrics == nil.
type periodRow struct {
	Period             *string
	AsOf               *time.Time
	PnL                *float64
	RealizedPnL        *float64
	ROI                *float64
	WinRate            *float64
	TradeCount         *int64
	Volume             *float64
	GrossProfit        *float64
	GrossLoss          *float64
	ProfitFactor       *float64
	AvgTradePnL        *float64
	LongCount          *int64
	LongWins           *int64
	ShortCount         *int64
	ShortWins          *int64
	MaxDrawdownPct     *float64
	AvgHoldingTimeSec  *float64
	LastTradeAt        *time.Time
	DataStatus         *string
	IsPartial          *bool
	CalculationVersion *int
}

func (row periodRow) toMetrics(venueID uuid.UUID, venue, addr string, display *string) *trader.PeriodMetrics {
	if row.Period == nil {
		return nil
	}
	m := &trader.PeriodMetrics{
		VenueID: venueID, Venue: venue, WalletAddress: addr, DisplayName: display,
		Period: *row.Period, PnL: row.PnL, RealizedPnL: row.RealizedPnL, ROI: row.ROI,
		WinRate: row.WinRate, TradeCount: row.TradeCount, Volume: row.Volume,
		GrossProfit: row.GrossProfit, GrossLoss: row.GrossLoss,
		ProfitFactor: row.ProfitFactor, AvgTradePnL: row.AvgTradePnL,
		LongCount: row.LongCount, LongWins: row.LongWins,
		ShortCount: row.ShortCount, ShortWins: row.ShortWins,
		MaxDrawdownPct: row.MaxDrawdownPct, AvgHoldingTimeSec: row.AvgHoldingTimeSec,
		LastTradeAt: row.LastTradeAt, IsPartial: row.IsPartial != nil && *row.IsPartial,
	}
	if row.AsOf != nil {
		m.AsOf = *row.AsOf
	}
	if row.DataStatus != nil {
		m.DataStatus = trader.DataStatus(*row.DataStatus)
	}
	if row.CalculationVersion != nil {
		m.CalculationVersion = *row.CalculationVersion
	}
	return m
}

// ListMembers returns owned-group memberships with venue codes, registry
// display names and period metrics (null when the wallet has none for the
// period). Empty period defaults to 30D; unknown values are INVALID_FILTER.
func (r *Repository) ListMembers(ctx context.Context, groupID, userID uuid.UUID, period string) ([]*Member, error) {
	if err := r.requireOwner(ctx, groupID, userID); err != nil {
		return nil, err
	}
	if period == "" {
		period = trader.Period30D
	}
	switch period {
	case trader.Period1D, trader.Period7D, trader.Period30D, trader.PeriodALL:
	default:
		return nil, domain.NewError(domain.ErrCodeInvalidFilter, "invalid period")
	}
	rows, err := r.pool.Query(ctx, `
		SELECT m.group_id, m.venue_id, v.code, m.wallet_address, r.display_name, m.alias, m.note,
			p.period, p.as_of, p.pnl, p.realized_pnl, p.roi, p.win_rate, p.trade_count,
			p.volume, p.gross_profit, p.gross_loss, p.profit_factor, p.avg_trade_pnl,
			p.long_count, p.long_wins, p.short_count, p.short_wins,
			p.max_drawdown_pct, p.avg_holding_time_sec, p.last_trade_at,
			p.data_status, p.is_partial, p.calculation_version
		FROM trader_group_members m
		JOIN venues v ON v.id = m.venue_id
		LEFT JOIN trader_registry r ON r.venue_id = m.venue_id AND r.wallet_address = m.wallet_address
		LEFT JOIN trader_period_metrics p ON p.venue_id = m.venue_id
			AND p.wallet_address = m.wallet_address AND p.period = $2
		WHERE m.group_id = $1 ORDER BY m.wallet_address ASC`, groupID, period)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Member{}
	for rows.Next() {
		var mb Member
		var prow periodRow
		if err := rows.Scan(&mb.GroupID, &mb.VenueID, &mb.Venue, &mb.WalletAddress,
			&mb.DisplayName, &mb.Alias, &mb.Note,
			&prow.Period, &prow.AsOf, &prow.PnL, &prow.RealizedPnL, &prow.ROI,
			&prow.WinRate, &prow.TradeCount, &prow.Volume, &prow.GrossProfit,
			&prow.GrossLoss, &prow.ProfitFactor, &prow.AvgTradePnL,
			&prow.LongCount, &prow.LongWins, &prow.ShortCount, &prow.ShortWins,
			&prow.MaxDrawdownPct, &prow.AvgHoldingTimeSec, &prow.LastTradeAt,
			&prow.DataStatus, &prow.IsPartial, &prow.CalculationVersion); err != nil {
			return nil, err
		}
		mb.Metrics = prow.toMetrics(mb.VenueID, mb.Venue, mb.WalletAddress, mb.DisplayName)
		out = append(out, &mb)
	}
	return out, rows.Err()
}

// OwnerOf returns the group's owner (for the search group_id filter).
func (r *Repository) OwnerOf(ctx context.Context, groupID uuid.UUID) (uuid.UUID, error) {
	var owner uuid.UUID
	err := r.pool.QueryRow(ctx,
		`SELECT user_id FROM trader_groups WHERE id = $1`, groupID).Scan(&owner)
	if err != nil {
		if err == pgx.ErrNoRows {
			return uuid.Nil, notFound()
		}
		return uuid.Nil, err
	}
	return owner, nil
}

// VenueIDByCode resolves a venue code to its id (unknown → 404).
func (r *Repository) VenueIDByCode(ctx context.Context, code string) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.pool.QueryRow(ctx,
		`SELECT id FROM venues WHERE code = $1`, code).Scan(&id)
	if err != nil {
		if err == pgx.ErrNoRows {
			return uuid.Nil, domain.NewError(domain.ErrCodeNotFound, "unknown venue: "+code)
		}
		return uuid.Nil, err
	}
	return id, nil
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "duplicate key")
}
