package trader

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// OverviewSnapshot is a precomputed overview payload row.
type OverviewSnapshot struct {
	VenueID    uuid.UUID
	Period     string
	PnlMetric  string
	ComputedAt time.Time
	Payload    []byte
}

// GetOverviewSnapshot loads a snapshot row (nil, nil when absent).
func (r *Repository) GetOverviewSnapshot(ctx context.Context, venueID uuid.UUID, period, pnlMetric string) (*OverviewSnapshot, error) {
	var s OverviewSnapshot
	err := r.pool.QueryRow(ctx, `
		SELECT venue_id, period, pnl_metric, computed_at, payload
		FROM trader_overview_snapshots
		WHERE venue_id = $1 AND period = $2 AND pnl_metric = $3`,
		venueID, period, pnlMetric).Scan(&s.VenueID, &s.Period, &s.PnlMetric, &s.ComputedAt, &s.Payload)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &s, nil
}

// UpsertOverviewSnapshot stores/updates a snapshot row.
func (r *Repository) UpsertOverviewSnapshot(ctx context.Context, venueID uuid.UUID, period, pnlMetric string, payload []byte, computedAt time.Time) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO trader_overview_snapshots (venue_id, period, pnl_metric, computed_at, payload)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (venue_id, period, pnl_metric) DO UPDATE SET
			computed_at = EXCLUDED.computed_at,
			payload = EXCLUDED.payload`,
		venueID, period, pnlMetric, computedAt, payload)
	return err
}

// VenueCodeByID resolves a venue id to its code (nil-friendly).
func (r *Repository) VenueCodeByID(ctx context.Context, venueID uuid.UUID) (string, error) {
	var code string
	err := r.pool.QueryRow(ctx, `SELECT code FROM venues WHERE id = $1`, venueID).Scan(&code)
	if err != nil {
		if err == pgx.ErrNoRows {
			return "", nil
		}
		return "", err
	}
	return code, nil
}
