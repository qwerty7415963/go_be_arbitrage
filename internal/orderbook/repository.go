package orderbook

import (
	"context"
	"encoding/json"
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

func (r *Repository) CreateSnapshot(ctx context.Context, snapshot *OrderBookSnapshot) error {
	bidsJSON, err := json.Marshal(snapshot.Bids)
	if err != nil {
		return err
	}

	asksJSON, err := json.Marshal(snapshot.Asks)
	if err != nil {
		return err
	}

	query := `
		INSERT INTO orderbook_snapshots (
			venue_id, instrument_id, sequence, best_bid, best_ask,
			bids, asks, timestamp
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, created_at`

	return r.db.QueryRow(ctx, query,
		snapshot.VenueID, snapshot.InstrumentID, snapshot.Sequence,
		snapshot.BestBid, snapshot.BestAsk, bidsJSON, asksJSON,
		snapshot.Timestamp,
	).Scan(&snapshot.ID, &snapshot.CreatedAt)
}

func (r *Repository) GetLatestSnapshot(ctx context.Context, venueID, instrumentID uuid.UUID) (*OrderBookSnapshot, error) {
	query := `
		SELECT id, venue_id, instrument_id, sequence, best_bid, best_ask,
			bids, asks, timestamp, created_at
		FROM orderbook_snapshots
		WHERE venue_id = $1 AND instrument_id = $2
		ORDER BY sequence DESC
		LIMIT 1`

	snapshot := &OrderBookSnapshot{}
	var bidsJSON, asksJSON []byte

	err := r.db.QueryRow(ctx, query, venueID, instrumentID).Scan(
		&snapshot.ID, &snapshot.VenueID, &snapshot.InstrumentID,
		&snapshot.Sequence, &snapshot.BestBid, &snapshot.BestAsk,
		&bidsJSON, &asksJSON, &snapshot.Timestamp, &snapshot.CreatedAt,
	)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(bidsJSON, &snapshot.Bids); err != nil {
		return nil, err
	}

	if err := json.Unmarshal(asksJSON, &snapshot.Asks); err != nil {
		return nil, err
	}

	return snapshot, nil
}

func (r *Repository) CreateDelta(ctx context.Context, delta *OrderBookDelta) error {
	bidsJSON, err := json.Marshal(delta.Bids)
	if err != nil {
		return err
	}

	asksJSON, err := json.Marshal(delta.Asks)
	if err != nil {
		return err
	}

	query := `
		INSERT INTO orderbook_deltas (
			venue_id, instrument_id, from_sequence, to_sequence,
			bids, asks, timestamp
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at`

	return r.db.QueryRow(ctx, query,
		delta.VenueID, delta.InstrumentID, delta.FromSequence,
		delta.ToSequence, bidsJSON, asksJSON, delta.Timestamp,
	).Scan(&delta.ID, &delta.CreatedAt)
}

func (r *Repository) GetDeltasSince(ctx context.Context, venueID, instrumentID uuid.UUID, sinceSequence int64) ([]*OrderBookDelta, error) {
	query := `
		SELECT id, venue_id, instrument_id, from_sequence, to_sequence,
			bids, asks, timestamp, created_at
		FROM orderbook_deltas
		WHERE venue_id = $1 AND instrument_id = $2 AND from_sequence >= $3
		ORDER BY from_sequence ASC`

	rows, err := r.db.Query(ctx, query, venueID, instrumentID, sinceSequence)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var deltas []*OrderBookDelta
	for rows.Next() {
		delta := &OrderBookDelta{}
		var bidsJSON, asksJSON []byte

		err := rows.Scan(
			&delta.ID, &delta.VenueID, &delta.InstrumentID,
			&delta.FromSequence, &delta.ToSequence,
			&bidsJSON, &asksJSON, &delta.Timestamp, &delta.CreatedAt,
		)
		if err != nil {
			return nil, err
		}

		if err := json.Unmarshal(bidsJSON, &delta.Bids); err != nil {
			return nil, err
		}

		if err := json.Unmarshal(asksJSON, &delta.Asks); err != nil {
			return nil, err
		}

		deltas = append(deltas, delta)
	}

	return deltas, rows.Err()
}

func (r *Repository) DeleteOldSnapshots(ctx context.Context, maxAge time.Duration) (int64, error) {
	query := `
		DELETE FROM orderbook_snapshots
		WHERE timestamp < NOW() - $1::interval`

	result, err := r.db.Exec(ctx, query, maxAge.String())
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

func (r *Repository) DeleteOldDeltas(ctx context.Context, maxAge time.Duration) (int64, error) {
	query := `
		DELETE FROM orderbook_deltas
		WHERE timestamp < NOW() - $1::interval`

	result, err := r.db.Exec(ctx, query, maxAge.String())
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}
