package fundingarbitrage

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

// GetPerpVenues returns all active venues that support perps
func (r *Repository) GetPerpVenues(ctx context.Context) ([]VenuePerp, error) {
	query := `
		SELECT id, code, name, venue_type
		FROM venues
		WHERE status = 'ACTIVE'
		ORDER BY name`

	rows, err := r.db.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var venues []VenuePerp
	for rows.Next() {
		var v VenuePerp
		if err := rows.Scan(&v.ID, &v.Code, &v.Name, &v.VenueType); err != nil {
			return nil, err
		}
		venues = append(venues, v)
	}
	return venues, rows.Err()
}

// GetVenuesByIDs returns venues by their IDs
func (r *Repository) GetVenuesByIDs(ctx context.Context, ids []uuid.UUID) ([]VenuePerp, error) {
	query := `
		SELECT id, code, name, venue_type
		FROM venues
		WHERE id = ANY($1) AND status = 'ACTIVE'
		ORDER BY name`

	rows, err := r.db.Query(ctx, query, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var venues []VenuePerp
	for rows.Next() {
		var v VenuePerp
		if err := rows.Scan(&v.ID, &v.Code, &v.Name, &v.VenueType); err != nil {
			return nil, err
		}
		venues = append(venues, v)
	}
	return venues, rows.Err()
}

// GetVenueInstrumentMappings returns venue-instrument mappings for given venues
func (r *Repository) GetVenueInstrumentMappings(ctx context.Context, venueIDs []uuid.UUID) ([]VenueInstrument, error) {
	query := `
		SELECT vi.venue_id, v.code, v.name, vi.instrument_id, i.base_asset, vi.venue_symbol
		FROM venue_instruments vi
		JOIN instruments i ON i.id = vi.instrument_id
		JOIN venues v ON v.id = vi.venue_id
		WHERE vi.venue_id = ANY($1)
		  AND i.instrument_type = 'PERP'
		  AND vi.status = 'ACTIVE'
		  AND v.status = 'ACTIVE'
		ORDER BY i.base_asset, v.code`

	rows, err := r.db.Query(ctx, query, venueIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var mappings []VenueInstrument
	for rows.Next() {
		var m VenueInstrument
		if err := rows.Scan(&m.VenueID, &m.VenueCode, &m.VenueName, &m.InstrumentID, &m.BaseAsset, &m.VenueSymbol); err != nil {
			return nil, err
		}
		mappings = append(mappings, m)
	}
	return mappings, rows.Err()
}

// GetLatestFunding returns the latest funding rate for a venue-instrument pair
func (r *Repository) GetLatestFunding(ctx context.Context, venueID, instrumentID uuid.UUID) (*FundingRecord, error) {
	query := `
		SELECT id, venue_id, instrument_id, observed_at, funding_rate, 
		       interval_seconds, mark_price, index_price, open_interest
		FROM funding_rates
		WHERE venue_id = $1 AND instrument_id = $2
		ORDER BY observed_at DESC
		LIMIT 1`

	var f FundingRecord
	err := r.db.QueryRow(ctx, query, venueID, instrumentID).Scan(
		&f.ID, &f.VenueID, &f.InstrumentID, &f.ObservedAt,
		&f.FundingRate, &f.IntervalSeconds, &f.MarkPrice, &f.IndexPrice, &f.OpenInterest,
	)
	if err != nil {
		return nil, err
	}
	return &f, nil
}

// GetFundingHistory returns funding history for a venue-instrument pair within a time window
func (r *Repository) GetFundingHistory(ctx context.Context, venueID, instrumentID uuid.UUID, since time.Time) ([]FundingRecord, error) {
	query := `
		SELECT id, venue_id, instrument_id, observed_at, funding_rate, 
		       interval_seconds, mark_price, index_price, open_interest
		FROM funding_rates
		WHERE venue_id = $1 AND instrument_id = $2 AND observed_at >= $3
		ORDER BY observed_at DESC`

	rows, err := r.db.Query(ctx, query, venueID, instrumentID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []FundingRecord
	for rows.Next() {
		var f FundingRecord
		if err := rows.Scan(&f.ID, &f.VenueID, &f.InstrumentID, &f.ObservedAt,
			&f.FundingRate, &f.IntervalSeconds, &f.MarkPrice, &f.IndexPrice, &f.OpenInterest); err != nil {
			return nil, err
		}
		records = append(records, f)
	}
	return records, rows.Err()
}
