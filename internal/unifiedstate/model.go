package unifiedstate

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

type VenueHealthStatus string

const (
	VenueHealthHealthy   VenueHealthStatus = "HEALTHY"
	VenueHealthStale     VenueHealthStatus = "STALE"
	VenueHealthUnhealthy VenueHealthStatus = "UNHEALTHY"
	VenueHealthUnknown   VenueHealthStatus = "UNKNOWN"
)

type PriceLevel struct {
	Price    string `json:"price"`
	Quantity string `json:"quantity"`
}

type DepthLevel struct {
	Price    string `json:"price"`
	Quantity string `json:"quantity"`
	Notional string `json:"notional"`
}

type FundingState struct {
	VenueID         uuid.UUID  `json:"venue_id"`
	FundingRate     string     `json:"funding_rate"`
	NextFundingAt   *time.Time `json:"next_funding_at"`
	IntervalSeconds int        `json:"interval_seconds"`
	MarkPrice       *string    `json:"mark_price"`
	IndexPrice      *string    `json:"index_price"`
	ReceivedAt      time.Time  `json:"received_at"`
}

type VenueMarketState struct {
	VenueID       uuid.UUID         `json:"venue_id"`
	InstrumentID  uuid.UUID         `json:"instrument_id"`
	VenueCode     string            `json:"venue_code"`
	BestBid       *string           `json:"best_bid"`
	BestAsk       *string           `json:"best_ask"`
	LastPrice     *string           `json:"last_price"`
	Spread        *string           `json:"spread"`
	BidDepth      []PriceLevel      `json:"bid_depth"`
	AskDepth      []PriceLevel      `json:"ask_depth"`
	Funding       *FundingState     `json:"funding"`
	MarkPrice     *string           `json:"mark_price"`
	IndexPrice    *string           `json:"index_price"`
	Health        VenueHealthStatus `json:"health"`
	LastUpdate    time.Time         `json:"last_update"`
	LastTicker    time.Time         `json:"last_ticker"`
	LastOrderbook time.Time         `json:"last_orderbook"`
	LastFunding   time.Time         `json:"last_funding"`
	AgeMs         int64             `json:"age_ms"`
	mu            sync.RWMutex      `json:"-"`
}

func (v *VenueMarketState) UpdateTicker(bestBid, bestAsk, lastPrice *string, spread *string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.BestBid = bestBid
	v.BestAsk = bestAsk
	v.LastPrice = lastPrice
	v.Spread = spread
	v.LastTicker = time.Now()
	v.LastUpdate = time.Now()
	v.AgeMs = 0
}

func (v *VenueMarketState) UpdateOrderBook(bids, asks []PriceLevel, bestBid, bestAsk *string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.BidDepth = bids
	v.AskDepth = asks
	if bestBid != nil {
		v.BestBid = bestBid
	}
	if bestAsk != nil {
		v.BestAsk = bestAsk
	}
	v.LastOrderbook = time.Now()
	v.LastUpdate = time.Now()
	v.AgeMs = 0
}

func (v *VenueMarketState) UpdateFunding(funding *FundingState) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.Funding = funding
	v.LastFunding = time.Now()
	v.LastUpdate = time.Now()
	v.AgeMs = 0
}

func (v *VenueMarketState) UpdateHealth(health VenueHealthStatus) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.Health = health
}

func (v *VenueMarketState) GetSnapshot() *VenueMarketState {
	v.mu.RLock()
	defer v.mu.RUnlock()

	snap := &VenueMarketState{
		VenueID:       v.VenueID,
		InstrumentID:  v.InstrumentID,
		VenueCode:     v.VenueCode,
		BestBid:       v.BestBid,
		BestAsk:       v.BestAsk,
		LastPrice:     v.LastPrice,
		Spread:        v.Spread,
		BidDepth:      make([]PriceLevel, len(v.BidDepth)),
		AskDepth:      make([]PriceLevel, len(v.AskDepth)),
		Funding:       v.Funding,
		MarkPrice:     v.MarkPrice,
		IndexPrice:    v.IndexPrice,
		Health:        v.Health,
		LastUpdate:    v.LastUpdate,
		LastTicker:    v.LastTicker,
		LastOrderbook: v.LastOrderbook,
		LastFunding:   v.LastFunding,
		AgeMs:         v.AgeMs,
	}
	copy(snap.BidDepth, v.BidDepth)
	copy(snap.AskDepth, v.AskDepth)
	return snap
}

type InstrumentState struct {
	ID              uuid.UUID                        `json:"id"`
	InstrumentID    uuid.UUID                        `json:"instrument_id"`
	CanonicalSymbol string                           `json:"canonical_symbol"`
	BaseAsset       string                           `json:"base_asset"`
	QuoteAsset      string                           `json:"quote_asset"`
	BestBid         *string                          `json:"best_bid"`
	BestBidVenue    *uuid.UUID                       `json:"best_bid_venue"`
	BestAsk         *string                          `json:"best_ask"`
	BestAskVenue    *uuid.UUID                       `json:"best_ask_venue"`
	Spread          *string                          `json:"spread"`
	LastPrice       *string                          `json:"last_price"`
	BidDepth        []DepthLevel                     `json:"bid_depth"`
	AskDepth        []DepthLevel                     `json:"ask_depth"`
	Funding         *FundingState                    `json:"funding"`
	FundingVenue    *uuid.UUID                       `json:"funding_venue"`
	MarkPrice       *string                          `json:"mark_price"`
	IndexPrice      *string                          `json:"index_price"`
	VenueStates     map[uuid.UUID]*VenueMarketState  `json:"venue_states"`
	HealthyVenues   int                              `json:"healthy_venues"`
	TotalVenues     int                              `json:"total_venues"`
	LastUpdate      time.Time                        `json:"last_update"`
	IsStale         bool                             `json:"is_stale"`
	CreatedAt       time.Time                        `json:"created_at"`
	UpdatedAt       time.Time                        `json:"updated_at"`
	mu              sync.RWMutex                     `json:"-"`
}

type UnifiedStateSnapshot struct {
	Instruments     map[uuid.UUID]*InstrumentState `json:"instruments"`
	TotalInstruments int                            `json:"total_instruments"`
	TotalHealthy    int                            `json:"total_healthy"`
	TotalStale      int                            `json:"total_stale"`
	Timestamp       time.Time                      `json:"timestamp"`
}

type ExecutableDepth struct {
	BestBid   *string      `json:"best_bid"`
	BestAsk   *string      `json:"best_ask"`
	Spread    *string      `json:"spread"`
	BidDepth  []DepthLevel `json:"bid_depth"`
	AskDepth  []DepthLevel `json:"ask_depth"`
	Timestamp time.Time    `json:"timestamp"`
}

type HealthOverview struct {
	TotalInstruments int                            `json:"total_instruments"`
	HealthyVenues    int                            `json:"healthy_venues"`
	StaleVenues      int                            `json:"stale_venues"`
	UnhealthyVenues  int                            `json:"unhealthy_venues"`
	VenueHealth      map[uuid.UUID]VenueHealthStatus `json:"venue_health"`
	Timestamp        time.Time                      `json:"timestamp"`
}
