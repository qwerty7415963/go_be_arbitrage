package opportunity

import (
	"time"

	"github.com/google/uuid"
)

type OpportunityType string

const (
	OpportunityTypePriceArb   OpportunityType = "PRICE_ARBITRAGE"
	OpportunityTypeFundingArb OpportunityType = "FUNDING_ARBITRAGE"
	OpportunityTypeBasisArb   OpportunityType = "BASIS_ARBITRAGE"
)

type OpportunityStatus string

const (
	OpportunityStatusDetected  OpportunityStatus = "DETECTED"
	OpportunityStatusExecuted  OpportunityStatus = "EXECUTED"
	OpportunityStatusExpired   OpportunityStatus = "EXPIRED"
	OpportunityStatusRejected  OpportunityStatus = "REJECTED"
	OpportunityStatusCancelled OpportunityStatus = "CANCELLED"
)

type LegSide string

const (
	LegSideBuy  LegSide = "BUY"
	LegSideSell LegSide = "SELL"
)

type MarketQuality string

const (
	MarketQualityGood     MarketQuality = "GOOD"
	MarketQualityDegraded MarketQuality = "DEGRADED"
	MarketQualityUnusable MarketQuality = "UNUSABLE"
)

type FeeModel struct {
	MakerFeeBPS int `json:"maker_fee_bps"`
	TakerFeeBPS int `json:"taker_fee_bps"`
}

func (f FeeModel) MakerFeePercent() float64 {
	return float64(f.MakerFeeBPS) / 10000.0
}

func (f FeeModel) TakerFeePercent() float64 {
	return float64(f.TakerFeeBPS) / 10000.0
}

type SlippageModel struct {
	MaxSlippageBPS int     `json:"max_slippage_bps"`
	DepthThreshold float64 `json:"depth_threshold"`
}

func (s SlippageModel) MaxSlippagePercent() float64 {
	return float64(s.MaxSlippageBPS) / 10000.0
}

type CapitalConstraints struct {
	MaxNotionalPerTrade string `json:"max_notional_per_trade"`
	MaxPositionUSD      string `json:"max_position_usd"`
	MaxLeverage         float64 `json:"max_leverage"`
	MinTradeSizeUSD     string `json:"min_trade_size_usd"`
}

type OpportunityLeg struct {
	ID              uuid.UUID  `json:"id"`
	OpportunityID   uuid.UUID  `json:"opportunity_id"`
	Side            LegSide    `json:"side"`
	VenueID         uuid.UUID  `json:"venue_id"`
	VenueCode       string     `json:"venue_code"`
	InstrumentID    uuid.UUID  `json:"instrument_id"`
	TargetSize      string     `json:"target_size"`
	TargetNotional  string     `json:"target_notional"`
	ExpectedPrice   string     `json:"expected_price"`
	ExpectedFunding *string    `json:"expected_funding,omitempty"`
	FeeBPS          int        `json:"fee_bps"`
	EstimatedSlippageBPS int   `json:"estimated_slippage_bps"`
	Metadata        *LegMeta  `json:"metadata,omitempty"`
}

type LegMeta struct {
	OrderBookDepth  int    `json:"order_book_depth,omitempty"`
	BestBid         string `json:"best_bid,omitempty"`
	BestAsk         string `json:"best_ask,omitempty"`
	Spread          string `json:"spread,omitempty"`
	HealthStatus    string `json:"health_status,omitempty"`
	LastUpdateAgeMs int64  `json:"last_update_age_ms,omitempty"`
}

type Opportunity struct {
	ID                   uuid.UUID          `json:"id"`
	TenantID             *uuid.UUID         `json:"tenant_id,omitempty"`
	OpportunityType      OpportunityType    `json:"opportunity_type"`
	Status               OpportunityStatus  `json:"status"`
	InstrumentID         uuid.UUID          `json:"instrument_id"`
	CanonicalSymbol      string             `json:"canonical_symbol"`
	DetectedAt           time.Time          `json:"detected_at"`
	ExpiresAt            time.Time          `json:"expires_at"`
	BuyVenueID           uuid.UUID          `json:"buy_venue_id"`
	BuyVenueCode         string             `json:"buy_venue_code"`
	SellVenueID          uuid.UUID          `json:"sell_venue_id"`
	SellVenueCode        string             `json:"sell_venue_code"`
	GrossEdge            string             `json:"gross_edge"`
	GrossEdgeBPS         int                `json:"gross_edge_bps"`
	EstimatedFees        string             `json:"estimated_fees"`
	EstimatedSlippage    string             `json:"estimated_slippage"`
	EstimatedOtherCosts  string             `json:"estimated_other_costs"`
	ExpectedNetEdge      string             `json:"expected_net_edge"`
	ExpectedNetEdgeBPS   int                `json:"expected_net_edge_bps"`
	ExpectedNetPnl       string             `json:"expected_net_pnl"`
	SuggestedSize        string             `json:"suggested_size"`
	SuggestedNotional    string             `json:"suggested_notional"`
	ExpectedHoldingSecs  int                `json:"expected_holding_secs"`
	Confidence           float64            `json:"confidence"`
	MarketQuality        MarketQuality      `json:"market_quality"`
	CalculationVersion   string             `json:"calculation_version"`
	MarketStateRef       *string            `json:"market_state_ref,omitempty"`
	Legs                 []*OpportunityLeg  `json:"legs"`
	CreatedAt            time.Time          `json:"created_at"`
	UpdatedAt            time.Time          `json:"updated_at"`
}

type ScanResult struct {
	Opportunities []*Opportunity `json:"opportunities"`
	ScannedAt     time.Time      `json:"scanned_at"`
	Duration      time.Duration  `json:"duration"`
	Instruments   int            `json:"instruments"`
	Venues        int            `json:"venues"`
}

type ScannerConfig struct {
	MinGrossEdgeBPS     int              `json:"min_gross_edge_bps"`
	MinNetEdgeBPS       int              `json:"min_net_edge_bps"`
	MinConfidence       float64          `json:"min_confidence"`
	OpportunityTTL      time.Duration    `json:"opportunity_ttl"`
	FeeModels           map[string]FeeModel `json:"fee_models"`
	SlippageModel       SlippageModel    `json:"slippage_model"`
	Capital             CapitalConstraints `json:"capital"`
	MaxDepthLevels      int              `json:"max_depth_levels"`
}

func DefaultScannerConfig() ScannerConfig {
	return ScannerConfig{
		MinGrossEdgeBPS: 10,
		MinNetEdgeBPS:   5,
		MinConfidence:   0.5,
		OpportunityTTL:  30 * time.Second,
		FeeModels: map[string]FeeModel{
			"binance":    {MakerFeeBPS: 2, TakerFeeBPS: 4},
			"extended":   {MakerFeeBPS: 0, TakerFeeBPS: 5},
			"variational": {MakerFeeBPS: 0, TakerFeeBPS: 0},
		},
		SlippageModel: SlippageModel{
			MaxSlippageBPS: 5,
			DepthThreshold: 0.01,
		},
		Capital: CapitalConstraints{
			MaxNotionalPerTrade: "100000",
			MaxPositionUSD:      "500000",
			MaxLeverage:         10,
			MinTradeSizeUSD:     "100",
		},
		MaxDepthLevels: 5,
	}
}
