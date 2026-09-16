package strategy

import (
	"time"

	"github.com/google/uuid"
)

type StrategyMode string

const (
	StrategyModePaper      StrategyMode = "PAPER"
	StrategyModeLiveManual StrategyMode = "LIVE_MANUAL"
	StrategyModeLiveAuto   StrategyMode = "LIVE_AUTO"
)

type StrategyStatus string

const (
	StrategyStatusDraft    StrategyStatus = "DRAFT"
	StrategyStatusRunning  StrategyStatus = "RUNNING"
	StrategyStatusPaused   StrategyStatus = "PAUSED"
	StrategyStatusDisabled StrategyStatus = "DISABLED"
	StrategyStatusError    StrategyStatus = "ERROR"
)

type StrategyType string

const (
	StrategyTypePriceArb   StrategyType = "PRICE_ARBITRAGE"
	StrategyTypeFundingArb StrategyType = "FUNDING_ARBITRAGE"
	StrategyTypeBasisArb   StrategyType = "BASIS_ARBITRAGE"
)

type StrategyInstance struct {
	ID             uuid.UUID      `json:"id"`
	TenantID       uuid.UUID      `json:"tenant_id"`
	StrategyTypeID uuid.UUID      `json:"strategy_type_id"`
	Name           string         `json:"name"`
	Mode           StrategyMode   `json:"mode"`
	Status         StrategyStatus `json:"status"`
	Config         *StrategyConfig `json:"config,omitempty"`
	CreatedBy      *uuid.UUID     `json:"created_by,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

type StrategyConfig struct {
	MinNetEdgeBPS       int              `json:"min_net_edge_bps"`
	MinConfidence       float64          `json:"min_confidence"`
	MaxNotionalPerTrade string           `json:"max_notional_per_trade"`
	MaxPositionUSD      string           `json:"max_position_usd"`
	MaxLeverage         float64          `json:"max_leverage"`
	Instruments         []uuid.UUID      `json:"instruments,omitempty"`
	Venues              []uuid.UUID      `json:"venues,omitempty"`
	CustomParams        map[string]string `json:"custom_params,omitempty"`
}

type StrategyDecision struct {
	ID            uuid.UUID   `json:"id"`
	StrategyID    uuid.UUID   `json:"strategy_id"`
	OpportunityID uuid.UUID   `json:"opportunity_id"`
	Action        string      `json:"action"`
	Status        string      `json:"status"`
	ExecutedAt    time.Time   `json:"executed_at"`
	CreatedAt     time.Time   `json:"created_at"`
}

type StrategyMetrics struct {
	StrategyID       uuid.UUID `json:"strategy_id"`
	TotalDecisions   int       `json:"total_decisions"`
	SuccessfulTrades int       `json:"successful_trades"`
	FailedTrades     int       `json:"failed_trades"`
	TotalPnL         string    `json:"total_pnl"`
	AverageEdge      float64   `json:"average_edge"`
	WinRate          float64   `json:"win_rate"`
}

type CreateStrategyRequest struct {
	Name   string          `json:"name" binding:"required"`
	Type   StrategyType    `json:"type" binding:"required"`
	Mode   StrategyMode    `json:"mode" binding:"required"`
	Config *StrategyConfig `json:"config"`
}

type UpdateStrategyRequest struct {
	Name   *string         `json:"name,omitempty"`
	Status *StrategyStatus `json:"status,omitempty"`
	Config *StrategyConfig `json:"config,omitempty"`
}
