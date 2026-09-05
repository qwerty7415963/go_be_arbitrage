package instrument

import (
	"time"

	"github.com/google/uuid"
)

type InstrumentType string

const (
	InstrumentTypeSpot   InstrumentType = "SPOT"
	InstrumentTypePerp   InstrumentType = "PERP"
	InstrumentTypeFuture InstrumentType = "FUTURE"
)

type ContractType string

const (
	ContractTypeLinear ContractType = "LINEAR"
	ContractTypeInverse ContractType = "INVERSE"
	ContractTypeSpot   ContractType = "SPOT"
	ContractTypeOther  ContractType = "OTHER"
)

type DiscoveryStatus string

const (
	DiscoveryStatusDiscovered DiscoveryStatus = "DISCOVERED"
	DiscoveryStatusReviewed   DiscoveryStatus = "REVIEWED"
	DiscoveryStatusRejected   DiscoveryStatus = "REJECTED"
)

type Instrument struct {
	ID                uuid.UUID       `json:"id" db:"id"`
	CanonicalSymbol   string          `json:"canonical_symbol" db:"canonical_symbol"`
	BaseAsset         string          `json:"base_asset" db:"base_asset"`
	QuoteAsset        string          `json:"quote_asset" db:"quote_asset"`
	InstrumentType    InstrumentType  `json:"instrument_type" db:"instrument_type"`
	ContractType      ContractType    `json:"contract_type" db:"contract_type"`
	ContractSize      *string         `json:"contract_size,omitempty" db:"contract_size"`
	PriceTick         string          `json:"price_tick" db:"price_tick"`
	QuantityStep      string          `json:"quantity_step" db:"quantity_step"`
	MinQuantity       *string         `json:"min_quantity,omitempty" db:"min_quantity"`
	MinNotional       *string         `json:"min_notional,omitempty" db:"min_notional"`
	MarginAsset       *string         `json:"margin_asset,omitempty" db:"margin_asset"`
	SettlementAsset   *string         `json:"settlement_asset,omitempty" db:"settlement_asset"`
	DiscoveryStatus   DiscoveryStatus `json:"discovery_status" db:"discovery_status"`
	TradingEnabled    bool            `json:"trading_enabled" db:"trading_enabled"`
	CreatedAt         time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at" db:"updated_at"`
}

type VenueInstrument struct {
	ID             uuid.UUID       `json:"id" db:"id"`
	VenueID        uuid.UUID       `json:"venue_id" db:"venue_id"`
	InstrumentID   uuid.UUID       `json:"instrument_id" db:"instrument_id"`
	VenueSymbol    string          `json:"venue_symbol" db:"venue_symbol"`
	Status         string          `json:"status" db:"status"`
	VenueMetadata  interface{}     `json:"venue_metadata" db:"venue_metadata"`
	FirstSeenAt    time.Time       `json:"first_seen_at" db:"first_seen_at"`
	LastSeenAt     time.Time       `json:"last_seen_at" db:"last_seen_at"`
}

type CreateInstrumentRequest struct {
	CanonicalSymbol string         `json:"canonical_symbol" binding:"required"`
	BaseAsset       string         `json:"base_asset" binding:"required"`
	QuoteAsset      string         `json:"quote_asset" binding:"required"`
	InstrumentType  InstrumentType `json:"instrument_type" binding:"required,oneof=SPOT PERP FUTURE"`
	ContractType    ContractType   `json:"contract_type" binding:"required,oneof=LINEAR INVERSE SPOT OTHER"`
	PriceTick       string         `json:"price_tick" binding:"required"`
	QuantityStep    string         `json:"quantity_step" binding:"required"`
	ContractSize    *string        `json:"contract_size"`
	MinQuantity     *string        `json:"min_quantity"`
	MinNotional     *string        `json:"min_notional"`
	MarginAsset     *string        `json:"margin_asset"`
	SettlementAsset *string        `json:"settlement_asset"`
}

type UpdateInstrumentRequest struct {
	PriceTick       *string            `json:"price_tick"`
	QuantityStep    *string            `json:"quantity_step"`
	ContractSize    *string            `json:"contract_size"`
	MinQuantity     *string            `json:"min_quantity"`
	MinNotional     *string            `json:"min_notional"`
	MarginAsset     *string            `json:"margin_asset"`
	SettlementAsset *string            `json:"settlement_asset"`
	DiscoveryStatus *DiscoveryStatus   `json:"discovery_status" binding:"omitempty,oneof=DISCOVERED REVIEWED REJECTED"`
}

type CreateVenueInstrumentRequest struct {
	VenueID        uuid.UUID `json:"venue_id" binding:"required"`
	InstrumentID   uuid.UUID `json:"instrument_id" binding:"required"`
	VenueSymbol    string    `json:"venue_symbol" binding:"required"`
}

type EnableTradingRequest struct {
	Enabled bool `json:"enabled"`
}
