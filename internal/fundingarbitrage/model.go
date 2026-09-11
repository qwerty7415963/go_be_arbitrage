package fundingarbitrage

import (
	"time"

	"github.com/google/uuid"
)

// VenueInfo represents venue information for API response
type VenueInfo struct {
	ID   uuid.UUID `json:"id"`
	Code string    `json:"code"`
	Name string    `json:"name"`
}

// FundingArbitrageResponse is the response for funding arbitrage endpoint
type FundingArbitrageResponse struct {
	DataAsOf    time.Time `json:"data_as_of"`
	CacheStatus string    `json:"cache_status"`
	Pairs       []Pair    `json:"pairs"`
}

// Pair represents a pair of venues with matching tokens
type Pair struct {
	VenueA VenueInfo        `json:"venue_a"`
	VenueB VenueInfo        `json:"venue_b"`
	Tokens []ArbitrageToken `json:"tokens"`
}

// ArbitrageToken represents a token with funding data from two venues
type ArbitrageToken struct {
	InstrumentID          uuid.UUID `json:"instrument_id"`
	Symbol                string    `json:"symbol"`
	VenueASymbol          string    `json:"venue_a_symbol"`
	VenueAFundingRate     string    `json:"venue_a_funding_rate"`
	VenueAIntervalSeconds int       `json:"venue_a_interval_seconds"`
	VenueAObservedAt      time.Time `json:"venue_a_observed_at"`
	VenueBSymbol          string    `json:"venue_b_symbol"`
	VenueBFundingRate     string    `json:"venue_b_funding_rate"`
	VenueBIntervalSeconds int       `json:"venue_b_interval_seconds"`
	VenueBObservedAt      time.Time `json:"venue_b_observed_at"`
	LongVenueID           uuid.UUID `json:"long_venue_id"`
	ShortVenueID          uuid.UUID `json:"short_venue_id"`
	APR1hPercent          *float64  `json:"apr_1h_percent"`
	APR4hPercent          *float64  `json:"apr_4h_percent"`
	APYPercent            *float64  `json:"apy_percent"`
	FundingAvailable      bool      `json:"funding_available"`
	IsStale               bool      `json:"is_stale"`
}

// VenueInstrument represents a venue-instrument mapping
type VenueInstrument struct {
	VenueID      uuid.UUID
	VenueCode    string
	VenueName    string
	InstrumentID uuid.UUID
	BaseAsset    string
	VenueSymbol  string
}

// FundingRecord represents a funding rate record from database
type FundingRecord struct {
	ID              int64
	VenueID         uuid.UUID
	InstrumentID    uuid.UUID
	ObservedAt      time.Time
	FundingRate     string
	IntervalSeconds int
	MarkPrice       string
	IndexPrice      string
}

// VenuePerp represents a venue that supports perps
type VenuePerp struct {
	ID        uuid.UUID `json:"id"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	VenueType string    `json:"venue_type"`
}
