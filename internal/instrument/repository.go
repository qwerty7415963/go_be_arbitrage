package instrument

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Create(ctx context.Context, inst *Instrument) error {
	var contractSize, minQty, minNotional, marginAsset, settlementAsset interface{}
	if inst.ContractSize != nil {
		contractSize = *inst.ContractSize
	}
	if inst.MinQuantity != nil {
		minQty = *inst.MinQuantity
	}
	if inst.MinNotional != nil {
		minNotional = *inst.MinNotional
	}
	if inst.MarginAsset != nil {
		marginAsset = *inst.MarginAsset
	}
	if inst.SettlementAsset != nil {
		settlementAsset = *inst.SettlementAsset
	}

	query := `
		INSERT INTO instruments (id, canonical_symbol, base_asset, quote_asset, instrument_type, 
			contract_type, contract_size, price_tick, quantity_step, min_quantity, min_notional, 
			margin_asset, settlement_asset, discovery_status, trading_enabled)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		RETURNING created_at, updated_at`

	return r.db.QueryRow(ctx, query,
		inst.ID, inst.CanonicalSymbol, inst.BaseAsset, inst.QuoteAsset, inst.InstrumentType,
		inst.ContractType, contractSize, inst.PriceTick, inst.QuantityStep, minQty, minNotional,
		marginAsset, settlementAsset, inst.DiscoveryStatus, inst.TradingEnabled,
	).Scan(&inst.CreatedAt, &inst.UpdatedAt)
}

func (r *Repository) GetByID(ctx context.Context, id uuid.UUID) (*Instrument, error) {
	query := `
		SELECT id, canonical_symbol, base_asset, quote_asset, instrument_type, 
			contract_type, contract_size, price_tick, quantity_step, min_quantity, min_notional, 
			margin_asset, settlement_asset, discovery_status, trading_enabled, created_at, updated_at
		FROM instruments
		WHERE id = $1`

	inst := &Instrument{}
	var contractSize, minQty, minNotional, marginAsset, settlementAsset *string

	err := r.db.QueryRow(ctx, query, id).Scan(
		&inst.ID, &inst.CanonicalSymbol, &inst.BaseAsset, &inst.QuoteAsset, &inst.InstrumentType,
		&inst.ContractType, &contractSize, &inst.PriceTick, &inst.QuantityStep, &minQty, &minNotional,
		&marginAsset, &settlementAsset, &inst.DiscoveryStatus, &inst.TradingEnabled, &inst.CreatedAt, &inst.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	inst.ContractSize = contractSize
	inst.MinQuantity = minQty
	inst.MinNotional = minNotional
	inst.MarginAsset = marginAsset
	inst.SettlementAsset = settlementAsset

	return inst, nil
}

func (r *Repository) GetByCanonicalSymbol(ctx context.Context, symbol string) (*Instrument, error) {
	query := `
		SELECT id, canonical_symbol, base_asset, quote_asset, instrument_type, 
			contract_type, contract_size, price_tick, quantity_step, min_quantity, min_notional, 
			margin_asset, settlement_asset, discovery_status, trading_enabled, created_at, updated_at
		FROM instruments
		WHERE canonical_symbol = $1`

	inst := &Instrument{}
	var contractSize, minQty, minNotional, marginAsset, settlementAsset *string

	err := r.db.QueryRow(ctx, query, symbol).Scan(
		&inst.ID, &inst.CanonicalSymbol, &inst.BaseAsset, &inst.QuoteAsset, &inst.InstrumentType,
		&inst.ContractType, &contractSize, &inst.PriceTick, &inst.QuantityStep, &minQty, &minNotional,
		&marginAsset, &settlementAsset, &inst.DiscoveryStatus, &inst.TradingEnabled, &inst.CreatedAt, &inst.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	inst.ContractSize = contractSize
	inst.MinQuantity = minQty
	inst.MinNotional = minNotional
	inst.MarginAsset = marginAsset
	inst.SettlementAsset = settlementAsset

	return inst, nil
}

func (r *Repository) List(ctx context.Context) ([]*Instrument, error) {
	query := `
		SELECT id, canonical_symbol, base_asset, quote_asset, instrument_type, 
			contract_type, contract_size, price_tick, quantity_step, min_quantity, min_notional, 
			margin_asset, settlement_asset, discovery_status, trading_enabled, created_at, updated_at
		FROM instruments
		ORDER BY created_at DESC`

	rows, err := r.db.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var instruments []*Instrument
	for rows.Next() {
		inst := &Instrument{}
		var contractSize, minQty, minNotional, marginAsset, settlementAsset *string

		err := rows.Scan(
			&inst.ID, &inst.CanonicalSymbol, &inst.BaseAsset, &inst.QuoteAsset, &inst.InstrumentType,
			&inst.ContractType, &contractSize, &inst.PriceTick, &inst.QuantityStep, &minQty, &minNotional,
			&marginAsset, &settlementAsset, &inst.DiscoveryStatus, &inst.TradingEnabled, &inst.CreatedAt, &inst.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}

		inst.ContractSize = contractSize
		inst.MinQuantity = minQty
		inst.MinNotional = minNotional
		inst.MarginAsset = marginAsset
		inst.SettlementAsset = settlementAsset

		instruments = append(instruments, inst)
	}
	return instruments, rows.Err()
}

func (r *Repository) ListTradable(ctx context.Context) ([]*Instrument, error) {
	query := `
		SELECT id, canonical_symbol, base_asset, quote_asset, instrument_type, 
			contract_type, contract_size, price_tick, quantity_step, min_quantity, min_notional, 
			margin_asset, settlement_asset, discovery_status, trading_enabled, created_at, updated_at
		FROM instruments
		WHERE trading_enabled = TRUE AND discovery_status = 'REVIEWED'
		ORDER BY created_at DESC`

	rows, err := r.db.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var instruments []*Instrument
	for rows.Next() {
		inst := &Instrument{}
		var contractSize, minQty, minNotional, marginAsset, settlementAsset *string

		err := rows.Scan(
			&inst.ID, &inst.CanonicalSymbol, &inst.BaseAsset, &inst.QuoteAsset, &inst.InstrumentType,
			&inst.ContractType, &contractSize, &inst.PriceTick, &inst.QuantityStep, &minQty, &minNotional,
			&marginAsset, &settlementAsset, &inst.DiscoveryStatus, &inst.TradingEnabled, &inst.CreatedAt, &inst.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}

		inst.ContractSize = contractSize
		inst.MinQuantity = minQty
		inst.MinNotional = minNotional
		inst.MarginAsset = marginAsset
		inst.SettlementAsset = settlementAsset

		instruments = append(instruments, inst)
	}
	return instruments, rows.Err()
}

func (r *Repository) Update(ctx context.Context, inst *Instrument) error {
	var contractSize, minQty, minNotional, marginAsset, settlementAsset interface{}
	if inst.ContractSize != nil {
		contractSize = *inst.ContractSize
	}
	if inst.MinQuantity != nil {
		minQty = *inst.MinQuantity
	}
	if inst.MinNotional != nil {
		minNotional = *inst.MinNotional
	}
	if inst.MarginAsset != nil {
		marginAsset = *inst.MarginAsset
	}
	if inst.SettlementAsset != nil {
		settlementAsset = *inst.SettlementAsset
	}

	query := `
		UPDATE instruments
		SET price_tick = $2, quantity_step = $3, contract_size = $4, min_quantity = $5, 
			min_notional = $6, margin_asset = $7, settlement_asset = $8, discovery_status = $9, 
			trading_enabled = $10, updated_at = NOW()
		WHERE id = $1
		RETURNING updated_at`

	return r.db.QueryRow(ctx, query,
		inst.ID, inst.PriceTick, inst.QuantityStep, contractSize, minQty,
		minNotional, marginAsset, settlementAsset, inst.DiscoveryStatus, inst.TradingEnabled,
	).Scan(&inst.UpdatedAt)
}

func (r *Repository) EnableTrading(ctx context.Context, id uuid.UUID, enabled bool) error {
	query := `
		UPDATE instruments
		SET trading_enabled = $2, updated_at = NOW()
		WHERE id = $1`
	_, err := r.db.Exec(ctx, query, id, enabled)
	return err
}

func (r *Repository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM instruments WHERE id = $1`
	_, err := r.db.Exec(ctx, query, id)
	return err
}

func (r *Repository) CreateVenueInstrument(ctx context.Context, vi *VenueInstrument) error {
	venueMetaJSON, err := json.Marshal(vi.VenueMetadata)
	if err != nil {
		return fmt.Errorf("marshal venue_metadata: %w", err)
	}

	query := `
		INSERT INTO venue_instruments (id, venue_id, instrument_id, venue_symbol, status, venue_metadata)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING first_seen_at, last_seen_at`

	return r.db.QueryRow(ctx, query,
		vi.ID, vi.VenueID, vi.InstrumentID, vi.VenueSymbol, vi.Status, venueMetaJSON,
	).Scan(&vi.FirstSeenAt, &vi.LastSeenAt)
}

func (r *Repository) GetVenueInstrument(ctx context.Context, venueID uuid.UUID, venueSymbol string) (*VenueInstrument, error) {
	query := `
		SELECT id, venue_id, instrument_id, venue_symbol, status, venue_metadata, first_seen_at, last_seen_at
		FROM venue_instruments
		WHERE venue_id = $1 AND venue_symbol = $2`

	vi := &VenueInstrument{}
	err := r.db.QueryRow(ctx, query, venueID, venueSymbol).Scan(
		&vi.ID, &vi.VenueID, &vi.InstrumentID, &vi.VenueSymbol, &vi.Status,
		&vi.VenueMetadata, &vi.FirstSeenAt, &vi.LastSeenAt,
	)
	if err != nil {
		return nil, err
	}
	return vi, nil
}

func (r *Repository) ListVenueInstruments(ctx context.Context, venueID uuid.UUID) ([]*VenueInstrument, error) {
	query := `
		SELECT id, venue_id, instrument_id, venue_symbol, status, venue_metadata, first_seen_at, last_seen_at
		FROM venue_instruments
		WHERE venue_id = $1
		ORDER BY first_seen_at DESC`

	rows, err := r.db.Query(ctx, query, venueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var venueInstruments []*VenueInstrument
	for rows.Next() {
		vi := &VenueInstrument{}
		err := rows.Scan(
			&vi.ID, &vi.VenueID, &vi.InstrumentID, &vi.VenueSymbol, &vi.Status,
			&vi.VenueMetadata, &vi.FirstSeenAt, &vi.LastSeenAt,
		)
		if err != nil {
			return nil, err
		}
		venueInstruments = append(venueInstruments, vi)
	}
	return venueInstruments, rows.Err()
}
