package market

import (
	"context"
	"crypto/sha256"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type RawEventRepository struct {
	db *pgxpool.Pool
}

func NewRawEventRepository(db *pgxpool.Pool) *RawEventRepository {
	return &RawEventRepository{db: db}
}

func (r *RawEventRepository) Create(ctx context.Context, event *RawMarketEvent) error {
	payloadHash := sha256.Sum256(event.Payload)
	event.PayloadHash = fmt.Sprintf("%x", payloadHash)

	query := `
		INSERT INTO raw_market_events (
			venue_id, venue_instrument_id, event_type, exchange_timestamp, 
			receive_timestamp, process_timestamp, exchange_sequence, 
			connection_id, payload, payload_hash
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id, created_at`

	return r.db.QueryRow(ctx, query,
		event.VenueID, event.VenueInstrumentID, event.EventType,
		event.ExchangeTimestamp, event.ReceiveTimestamp, event.ProcessTimestamp,
		event.ExchangeSequence, event.ConnectionID, event.Payload, event.PayloadHash,
	).Scan(&event.ID, &event.CreatedAt)
}

func (r *RawEventRepository) GetByID(ctx context.Context, id int64) (*RawMarketEvent, error) {
	query := `
		SELECT id, venue_id, venue_instrument_id, event_type, exchange_timestamp, 
			receive_timestamp, process_timestamp, exchange_sequence, 
			connection_id, payload, payload_hash, created_at
		FROM raw_market_events
		WHERE id = $1`

	event := &RawMarketEvent{}
	err := r.db.QueryRow(ctx, query, id).Scan(
		&event.ID, &event.VenueID, &event.VenueInstrumentID, &event.EventType,
		&event.ExchangeTimestamp, &event.ReceiveTimestamp, &event.ProcessTimestamp,
		&event.ExchangeSequence, &event.ConnectionID, &event.Payload,
		&event.PayloadHash, &event.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return event, nil
}

func (r *RawEventRepository) ListByVenue(ctx context.Context, venueID uuid.UUID, limit int) ([]*RawMarketEvent, error) {
	query := `
		SELECT id, venue_id, venue_instrument_id, event_type, exchange_timestamp, 
			receive_timestamp, process_timestamp, exchange_sequence, 
			connection_id, payload, payload_hash, created_at
		FROM raw_market_events
		WHERE venue_id = $1
		ORDER BY receive_timestamp DESC
		LIMIT $2`

	rows, err := r.db.Query(ctx, query, venueID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []*RawMarketEvent
	for rows.Next() {
		event := &RawMarketEvent{}
		err := rows.Scan(
			&event.ID, &event.VenueID, &event.VenueInstrumentID, &event.EventType,
			&event.ExchangeTimestamp, &event.ReceiveTimestamp, &event.ProcessTimestamp,
			&event.ExchangeSequence, &event.ConnectionID, &event.Payload,
			&event.PayloadHash, &event.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (r *RawEventRepository) ListByVenueInstrument(ctx context.Context, venueID, instrumentID uuid.UUID, limit int) ([]*RawMarketEvent, error) {
	query := `
		SELECT id, venue_id, venue_instrument_id, event_type, exchange_timestamp, 
			receive_timestamp, process_timestamp, exchange_sequence, 
			connection_id, payload, payload_hash, created_at
		FROM raw_market_events
		WHERE venue_id = $1 AND venue_instrument_id = $2
		ORDER BY receive_timestamp DESC
		LIMIT $3`

	rows, err := r.db.Query(ctx, query, venueID, instrumentID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []*RawMarketEvent
	for rows.Next() {
		event := &RawMarketEvent{}
		err := rows.Scan(
			&event.ID, &event.VenueID, &event.VenueInstrumentID, &event.EventType,
			&event.ExchangeTimestamp, &event.ReceiveTimestamp, &event.ProcessTimestamp,
			&event.ExchangeSequence, &event.ConnectionID, &event.Payload,
			&event.PayloadHash, &event.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (r *RawEventRepository) DeleteByAge(ctx context.Context, maxAge int) (int64, error) {
	query := `
		DELETE FROM raw_market_events
		WHERE receive_timestamp < NOW() - INTERVAL '1 day' * $1`

	result, err := r.db.Exec(ctx, query, maxAge)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}
