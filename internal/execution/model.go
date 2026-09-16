package execution

import (
	"time"

	"github.com/google/uuid"
)

type ExecutionMode string

const (
	ExecutionModePaper     ExecutionMode = "PAPER"
	ExecutionModeLiveManual ExecutionMode = "LIVE_MANUAL"
	ExecutionModeLiveAuto   ExecutionMode = "LIVE_AUTO"
)

type ExecutionStatus string

const (
	ExecutionStatusCreated       ExecutionStatus = "CREATED"
	ExecutionStatusRiskApproved  ExecutionStatus = "RISK_APPROVED"
	ExecutionStatusAwaitingApproval ExecutionStatus = "AWAITING_APPROVAL"
	ExecutionStatusPlanned       ExecutionStatus = "PLANNED"
	ExecutionStatusSubmitting    ExecutionStatus = "SUBMITTING"
	ExecutionStatusPartial       ExecutionStatus = "PARTIAL"
	ExecutionStatusCompleted     ExecutionStatus = "COMPLETED"
	ExecutionStatusRecovery      ExecutionStatus = "RECOVERY"
	ExecutionStatusFailed        ExecutionStatus = "FAILED"
	ExecutionStatusCanceled      ExecutionStatus = "CANCELED"
)

type LegStatus string

const (
	LegStatusPending    LegStatus = "PENDING"
	LegStatusSubmitting LegStatus = "SUBMITTING"
	LegStatusOpen       LegStatus = "OPEN"
	LegStatusPartial    LegStatus = "PARTIAL"
	LegStatusFilled     LegStatus = "FILLED"
	LegStatusFailed     LegStatus = "FAILED"
	LegStatusCanceled   LegStatus = "CANCELED"
	LegStatusRecovery   LegStatus = "RECOVERY"
)

type OrderStatus string

const (
	OrderStatusCreated          OrderStatus = "CREATED"
	OrderStatusSubmitting       OrderStatus = "SUBMITTING"
	OrderStatusOpen             OrderStatus = "OPEN"
	OrderStatusPartiallyFilled  OrderStatus = "PARTIALLY_FILLED"
	OrderStatusFilled           OrderStatus = "FILLED"
	OrderStatusCanceling        OrderStatus = "CANCELING"
	OrderStatusCanceled         OrderStatus = "CANCELED"
	OrderStatusRejected         OrderStatus = "REJECTED"
	OrderStatusExpired          OrderStatus = "EXPIRED"
	OrderStatusUnknown          OrderStatus = "UNKNOWN"
)

type Side string

const (
	SideBuy  Side = "BUY"
	SideSell Side = "SELL"
)

type Execution struct {
	ID                uuid.UUID       `json:"id"`
	TenantID          uuid.UUID       `json:"tenant_id"`
	StrategyInstanceID uuid.UUID      `json:"strategy_instance_id"`
	OpportunityID     *uuid.UUID      `json:"opportunity_id,omitempty"`
	ExecutionMode     ExecutionMode   `json:"execution_mode"`
	Status            ExecutionStatus `json:"status"`
	IntentJSON        interface{}     `json:"intent_json"`
	ExecutionPlanJSON interface{}     `json:"execution_plan_json,omitempty"`
	RiskDecisionID    *uuid.UUID      `json:"risk_decision_id,omitempty"`
	CreatedAt         time.Time       `json:"created_at"`
	CompletedAt       *time.Time      `json:"completed_at,omitempty"`
	FinalPnL          *float64        `json:"final_pnl,omitempty"`
}

type ExecutionLeg struct {
	ID             uuid.UUID  `json:"id"`
	ExecutionID    uuid.UUID  `json:"execution_id"`
	LegIndex       int        `json:"leg_index"`
	LegRole        string     `json:"leg_role"`
	VenueAccountID uuid.UUID  `json:"venue_account_id"`
	InstrumentID   uuid.UUID  `json:"instrument_id"`
	TargetSide     Side       `json:"target_side"`
	TargetQuantity *float64   `json:"target_quantity,omitempty"`
	TargetNotional *float64   `json:"target_notional,omitempty"`
	Status         LegStatus  `json:"status"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
	Metadata       interface{} `json:"metadata,omitempty"`
}

type Order struct {
	ID                  uuid.UUID   `json:"id"`
	ExecutionLegID      *uuid.UUID  `json:"execution_leg_id,omitempty"`
	VenueAccountID      uuid.UUID   `json:"venue_account_id"`
	InstrumentID        uuid.UUID   `json:"instrument_id"`
	ClientOrderID       string      `json:"client_order_id"`
	ExchangeOrderID     *string     `json:"exchange_order_id,omitempty"`
	Side                Side        `json:"side"`
	OrderType           string      `json:"order_type"`
	TimeInForce         *string     `json:"time_in_force,omitempty"`
	RequestedQuantity   *float64    `json:"requested_quantity,omitempty"`
	RequestedPrice      *float64    `json:"requested_price,omitempty"`
	Status              OrderStatus `json:"status"`
	SubmitTimestamp     *time.Time  `json:"submit_timestamp,omitempty"`
	AckTimestamp        *time.Time  `json:"ack_timestamp,omitempty"`
	CancelTimestamp     *time.Time  `json:"cancel_timestamp,omitempty"`
	LastExchangeUpdate  *time.Time  `json:"last_exchange_update_at,omitempty"`
	CreatedAt           time.Time   `json:"created_at"`
	UpdatedAt           time.Time   `json:"updated_at"`
	LastErrorCode       *string     `json:"last_error_code,omitempty"`
}

type Fill struct {
	ID             uuid.UUID  `json:"id"`
	OrderID        uuid.UUID  `json:"order_id"`
	VenueAccountID uuid.UUID  `json:"venue_account_id"`
	ExchangeFillID *string    `json:"exchange_fill_id,omitempty"`
	FilledAt       time.Time  `json:"filled_at"`
	Quantity       float64    `json:"quantity"`
	Price          float64    `json:"price"`
	FeeAmount      *float64   `json:"fee_amount,omitempty"`
	FeeAsset       *string    `json:"fee_asset,omitempty"`
	LiquidityRole  *string    `json:"liquidity_role,omitempty"`
}

type ExecutionIntent struct {
	StrategyID     uuid.UUID     `json:"strategy_id"`
	OpportunityID  *uuid.UUID    `json:"opportunity_id,omitempty"`
	Legs           []IntentLeg   `json:"legs"`
}

type IntentLeg struct {
	VenueAccountID uuid.UUID `json:"venue_account_id"`
	InstrumentID   uuid.UUID `json:"instrument_id"`
	Side           Side      `json:"side"`
	Quantity       float64   `json:"quantity"`
	Price          float64   `json:"price"`
}

type CreateExecutionRequest struct {
	StrategyInstanceID uuid.UUID       `json:"strategy_instance_id" binding:"required"`
	ExecutionMode      ExecutionMode   `json:"execution_mode" binding:"required"`
	Intent             ExecutionIntent `json:"intent" binding:"required"`
}

type UpdateExecutionRequest struct {
	Status *ExecutionStatus `json:"status,omitempty"`
}
