package risk

import (
	"time"

	"github.com/google/uuid"
)

type PolicyType string

const (
	PolicyTypeGlobal     PolicyType = "GLOBAL"
	PolicyTypeStrategy   PolicyType = "STRATEGY"
	PolicyTypeVenue      PolicyType = "VENUE"
	PolicyTypeAccount    PolicyType = "ACCOUNT"
	PolicyTypeInstrument PolicyType = "INSTRUMENT"
)

type PolicyStatus string

const (
	PolicyStatusActive   PolicyStatus = "ACTIVE"
	PolicyStatusDisabled PolicyStatus = "DISABLED"
)

type CheckResult string

const (
	CheckResultPass CheckResult = "PASS"
	CheckResultFail CheckResult = "FAIL"
	CheckResultWarn CheckResult = "WARN"
	CheckResultSkip CheckResult = "SKIP"
)

type RiskAction string

const (
	RiskActionApprove RiskAction = "APPROVE"
	RiskActionReject  RiskAction = "REJECT"
	RiskActionRestrict RiskAction = "RESTRICT"
	RiskActionHalt    RiskAction = "HALT"
)

type RiskPolicy struct {
	ID        uuid.UUID      `json:"id"`
	TenantID  uuid.UUID      `json:"tenant_id"`
	Name      string         `json:"name"`
	PolicyType PolicyType    `json:"policy_type"`
	Config    RiskConfig     `json:"config"`
	Status    PolicyStatus   `json:"status"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

type RiskConfig struct {
	MaxPositionPerInstrument string  `json:"max_position_per_instrument"`
	MaxNotionalPerTrade      string  `json:"max_notional_per_trade"`
	MaxLeverage              float64 `json:"max_leverage"`
	MaxPortfolioExposure     string  `json:"max_portfolio_exposure"`
	MaxVenueConcentration    float64 `json:"max_venue_concentration"`
	MinMarketDepthUSD        string  `json:"min_market_depth_usd"`
	MaxFundingRatePercent    float64 `json:"max_funding_rate_percent"`
	KillSwitchEnabled        bool    `json:"kill_switch_enabled"`
}

type RiskCheck struct {
	ID              uuid.UUID   `json:"id"`
	RiskDecisionID  *uuid.UUID  `json:"risk_decision_id,omitempty"`
	CheckType       string      `json:"check_type"`
	Result          CheckResult `json:"result"`
	ObservedValue   interface{} `json:"observed_value"`
	ThresholdValue  interface{} `json:"threshold_value,omitempty"`
	ReasonCode      *string     `json:"reason_code,omitempty"`
	CreatedAt       time.Time   `json:"created_at"`
}

type PreTradeCheckRequest struct {
	TenantID       uuid.UUID `json:"tenant_id"`
	StrategyID     uuid.UUID `json:"strategy_id"`
	VenueAccountID uuid.UUID `json:"venue_account_id"`
	InstrumentID   uuid.UUID `json:"instrument_id"`
	Side           string    `json:"side"`
	Quantity       float64   `json:"quantity"`
	Price          float64   `json:"price"`
	Notional       float64   `json:"notional"`
}

type PreTradeCheckResult struct {
	Action   RiskAction  `json:"action"`
	Checks   []*RiskCheck `json:"checks"`
	Reasons  []string    `json:"reasons,omitempty"`
}

type KillSwitch struct {
	Enabled   bool      `json:"enabled"`
	EnabledAt *time.Time `json:"enabled_at,omitempty"`
	Reason    string    `json:"reason,omitempty"`
}

func DefaultRiskConfig() RiskConfig {
	return RiskConfig{
		MaxPositionPerInstrument: "100000",
		MaxNotionalPerTrade:      "50000",
		MaxLeverage:              10,
		MaxPortfolioExposure:     "500000",
		MaxVenueConcentration:    0.5,
		MinMarketDepthUSD:        "10000",
		MaxFundingRatePercent:    1.0,
		KillSwitchEnabled:        false,
	}
}

type CreatePolicyRequest struct {
	Name       string     `json:"name" binding:"required"`
	PolicyType PolicyType `json:"policy_type" binding:"required"`
	Config     RiskConfig `json:"config"`
}

type UpdatePolicyRequest struct {
	Name   *string       `json:"name,omitempty"`
	Status *PolicyStatus `json:"status,omitempty"`
	Config *RiskConfig   `json:"config,omitempty"`
}
