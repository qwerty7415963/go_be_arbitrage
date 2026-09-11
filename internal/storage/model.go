package storage

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

type MarketQuality string

const (
	MarketQualityGood     MarketQuality = "GOOD"
	MarketQualityDegraded MarketQuality = "DEGRADED"
	MarketQualityUnusable MarketQuality = "UNUSABLE"
)

type Opportunity struct {
	ID                  uuid.UUID         `json:"id" db:"id"`
	TenantID            uuid.UUID         `json:"tenant_id" db:"tenant_id"`
	StrategyTypeID      uuid.UUID         `json:"strategy_type_id" db:"strategy_type_id"`
	InstrumentID        uuid.UUID         `json:"instrument_id" db:"instrument_id"`
	OpportunityType     OpportunityType   `json:"opportunity_type" db:"opportunity_type"`
	DetectedAt          time.Time         `json:"detected_at" db:"detected_at"`
	ExpiresAt           time.Time         `json:"expires_at" db:"expires_at"`
	BuyVenueAccountID   *uuid.UUID        `json:"buy_venue_account_id" db:"buy_venue_account_id"`
	SellVenueAccountID  *uuid.UUID        `json:"sell_venue_account_id" db:"sell_venue_account_id"`
	SuggestedSize       *string           `json:"suggested_size" db:"suggested_size"`
	SuggestedNotional   *string           `json:"suggested_notional" db:"suggested_notional"`
	ExecutableBuyPrice  *string           `json:"executable_buy_price" db:"executable_buy_price"`
	ExecutableSellPrice *string           `json:"executable_sell_price" db:"executable_sell_price"`
	GrossEdge           *string           `json:"gross_edge" db:"gross_edge"`
	EstimatedFees       *string           `json:"estimated_fees" db:"estimated_fees"`
	EstimatedSlippage   *string           `json:"estimated_slippage" db:"estimated_slippage"`
	EstimatedOtherCosts *string           `json:"estimated_other_costs" db:"estimated_other_costs"`
	ExpectedNetEdge     *string           `json:"expected_net_edge" db:"expected_net_edge"`
	ExpectedNetPnl      *string           `json:"expected_net_pnl" db:"expected_net_pnl"`
	ExpectedHoldingSecs *int              `json:"expected_holding_seconds" db:"expected_holding_seconds"`
	Confidence          *string           `json:"confidence" db:"confidence"`
	MarketQuality       MarketQuality     `json:"market_quality" db:"market_quality"`
	CalculationVersion  string            `json:"calculation_version" db:"calculation_version"`
	MarketStateRef      *string           `json:"market_state_ref" db:"market_state_ref"`
	Payload             interface{}       `json:"payload" db:"payload"`
	Legs                []*OpportunityLeg `json:"legs,omitempty" db:"-"`
	CreatedAt           time.Time         `json:"created_at" db:"created_at"`
	UpdatedAt           time.Time         `json:"updated_at" db:"updated_at"`
}

type OpportunityLeg struct {
	ID              uuid.UUID   `json:"id" db:"id"`
	OpportunityID   uuid.UUID   `json:"opportunity_id" db:"opportunity_id"`
	LegRole         string      `json:"leg_role" db:"leg_role"`
	VenueAccountID  uuid.UUID   `json:"venue_account_id" db:"venue_account_id"`
	InstrumentID    uuid.UUID   `json:"instrument_id" db:"instrument_id"`
	TargetSize      *string     `json:"target_size" db:"target_size"`
	TargetNotional  *string     `json:"target_notional" db:"target_notional"`
	ExpectedPrice   *string     `json:"expected_price" db:"expected_price"`
	ExpectedFunding *string     `json:"expected_funding" db:"expected_funding"`
	Metadata        interface{} `json:"metadata" db:"metadata"`
}

type StrategyDecision struct {
	ID                  uuid.UUID   `json:"id" db:"id"`
	OpportunityID       *uuid.UUID  `json:"opportunity_id" db:"opportunity_id"`
	StrategyInstanceID  uuid.UUID   `json:"strategy_instance_id" db:"strategy_instance_id"`
	Decision            string      `json:"decision" db:"decision"`
	ReasonCode          string      `json:"reason_code" db:"reason_code"`
	ReasonDetail        interface{} `json:"reason_detail" db:"reason_detail"`
	ConfigVersionID     *uuid.UUID  `json:"config_version_id" db:"config_version_id"`
	DecisionTimestamp   time.Time   `json:"decision_timestamp" db:"decision_timestamp"`
	InputMarketStateRef *string     `json:"input_market_state_ref" db:"input_market_state_ref"`
}

type RiskDecision struct {
	ID                  uuid.UUID   `json:"id" db:"id"`
	ExecutionID         *uuid.UUID  `json:"execution_id" db:"execution_id"`
	OpportunityID       *uuid.UUID  `json:"opportunity_id" db:"opportunity_id"`
	StrategyInstanceID  uuid.UUID   `json:"strategy_instance_id" db:"strategy_instance_id"`
	Decision            string      `json:"decision" db:"decision"`
	ReasonCode          string      `json:"reason_code" db:"reason_code"`
	ReasonDetail        interface{} `json:"reason_detail" db:"reason_detail"`
	RiskPolicyVersionID *uuid.UUID  `json:"risk_policy_version_id" db:"risk_policy_version_id"`
	EvaluatedAt         time.Time   `json:"evaluated_at" db:"evaluated_at"`
}

type AuditEvent struct {
	ID            int64       `json:"id" db:"id"`
	TenantID      *uuid.UUID  `json:"tenant_id" db:"tenant_id"`
	ActorType     string      `json:"actor_type" db:"actor_type"`
	ActorID       *string     `json:"actor_id" db:"actor_id"`
	Action        string      `json:"action" db:"action"`
	EntityType    *string     `json:"entity_type" db:"entity_type"`
	EntityID      *string     `json:"entity_id" db:"entity_id"`
	OccurredAt    time.Time   `json:"occurred_at" db:"occurred_at"`
	CorrelationID *uuid.UUID  `json:"correlation_id" db:"correlation_id"`
	Payload       interface{} `json:"payload" db:"payload"`
}

type SystemEvent struct {
	ID            int64       `json:"id" db:"id"`
	TenantID      *uuid.UUID  `json:"tenant_id" db:"tenant_id"`
	EventType     string      `json:"event_type" db:"event_type"`
	OccurredAt    time.Time   `json:"occurred_at" db:"occurred_at"`
	CorrelationID *uuid.UUID  `json:"correlation_id" db:"correlation_id"`
	Payload       interface{} `json:"payload" db:"payload"`
}
