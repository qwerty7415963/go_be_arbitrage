package unifiedstate

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

type InstrumentStateRecord struct {
	ID              uuid.UUID `json:"id" db:"id"`
	InstrumentID    uuid.UUID `json:"instrument_id" db:"instrument_id"`
	CanonicalSymbol string    `json:"canonical_symbol" db:"canonical_symbol"`
	BestBid         *string   `json:"best_bid" db:"best_bid"`
	BestAsk         *string   `json:"best_ask" db:"best_ask"`
	Spread          *string   `json:"spread" db:"spread"`
	LastPrice       *string   `json:"last_price" db:"last_price"`
	BidDepth        []byte    `json:"bid_depth" db:"bid_depth"`
	AskDepth        []byte    `json:"ask_depth" db:"ask_depth"`
	Funding         []byte    `json:"funding" db:"funding"`
	MarkPrice       *string   `json:"mark_price" db:"mark_price"`
	IndexPrice      *string   `json:"index_price" db:"index_price"`
	HealthyVenues   int       `json:"healthy_venues" db:"healthy_venues"`
	TotalVenues     int       `json:"total_venues" db:"total_venues"`
	IsStale         bool      `json:"is_stale" db:"is_stale"`
	LastUpdate      time.Time `json:"last_update" db:"last_update"`
	CreatedAt       time.Time `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time `json:"updated_at" db:"updated_at"`
}

func (r *Repository) UpsertInstrumentState(ctx context.Context, state *InstrumentState) error {
	bidDepthJSON, _ := json.Marshal(state.BidDepth)
	askDepthJSON, _ := json.Marshal(state.AskDepth)
	fundingJSON, _ := json.Marshal(state.Funding)

	query := `
		INSERT INTO unified_instrument_states (
			id, instrument_id, canonical_symbol, best_bid, best_ask,
			spread, last_price, bid_depth, ask_depth, funding,
			mark_price, index_price, healthy_venues, total_venues, is_stale, last_update
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
		ON CONFLICT (instrument_id) DO UPDATE SET
			best_bid = EXCLUDED.best_bid,
			best_ask = EXCLUDED.best_ask,
			spread = EXCLUDED.spread,
			last_price = EXCLUDED.last_price,
			bid_depth = EXCLUDED.bid_depth,
			ask_depth = EXCLUDED.ask_depth,
			funding = EXCLUDED.funding,
			mark_price = EXCLUDED.mark_price,
			index_price = EXCLUDED.index_price,
			healthy_venues = EXCLUDED.healthy_venues,
			total_venues = EXCLUDED.total_venues,
			is_stale = EXCLUDED.is_stale,
			last_update = EXCLUDED.last_update,
			updated_at = NOW()
		RETURNING id, created_at, updated_at`

	return r.db.QueryRow(ctx, query,
		state.ID, state.InstrumentID, state.CanonicalSymbol,
		state.BestBid, state.BestAsk, state.Spread, state.LastPrice,
		bidDepthJSON, askDepthJSON, fundingJSON,
		state.MarkPrice, state.IndexPrice,
		state.HealthyVenues, state.TotalVenues, state.IsStale, state.LastUpdate,
	).Scan(&state.ID, &state.CreatedAt, &state.UpdatedAt)
}

func (r *Repository) GetInstrumentState(ctx context.Context, instrumentID uuid.UUID) (*InstrumentStateRecord, error) {
	query := `
		SELECT id, instrument_id, canonical_symbol, best_bid, best_ask,
			spread, last_price, bid_depth, ask_depth, funding,
			mark_price, index_price, healthy_venues, total_venues, is_stale,
			last_update, created_at, updated_at
		FROM unified_instrument_states
		WHERE instrument_id = $1`

	record := &InstrumentStateRecord{}
	err := r.db.QueryRow(ctx, query, instrumentID).Scan(
		&record.ID, &record.InstrumentID, &record.CanonicalSymbol,
		&record.BestBid, &record.BestAsk, &record.Spread, &record.LastPrice,
		&record.BidDepth, &record.AskDepth, &record.Funding,
		&record.MarkPrice, &record.IndexPrice,
		&record.HealthyVenues, &record.TotalVenues, &record.IsStale,
		&record.LastUpdate, &record.CreatedAt, &record.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return record, nil
}

func (r *Repository) ListInstrumentStates(ctx context.Context) ([]*InstrumentStateRecord, error) {
	query := `
		SELECT id, instrument_id, canonical_symbol, best_bid, best_ask,
			spread, last_price, bid_depth, ask_depth, funding,
			mark_price, index_price, healthy_venues, total_venues, is_stale,
			last_update, created_at, updated_at
		FROM unified_instrument_states
		ORDER BY canonical_symbol ASC`

	rows, err := r.db.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []*InstrumentStateRecord
	for rows.Next() {
		record := &InstrumentStateRecord{}
		err := rows.Scan(
			&record.ID, &record.InstrumentID, &record.CanonicalSymbol,
			&record.BestBid, &record.BestAsk, &record.Spread, &record.LastPrice,
			&record.BidDepth, &record.AskDepth, &record.Funding,
			&record.MarkPrice, &record.IndexPrice,
			&record.HealthyVenues, &record.TotalVenues, &record.IsStale,
			&record.LastUpdate, &record.CreatedAt, &record.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}
