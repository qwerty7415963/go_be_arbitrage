package reconciliation

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type TriggerSource string

const (
	TriggerStartup          TriggerSource = "STARTUP"
	TriggerReconnect        TriggerSource = "RECONNECT"
	TriggerScheduled        TriggerSource = "SCHEDULED"
	TriggerManual           TriggerSource = "MANUAL"
	TriggerExecutionRecovery TriggerSource = "EXECUTION_RECOVERY"
)

type RunStatus string

const (
	RunStatusRunning  RunStatus = "RUNNING"
	RunStatusMatched  RunStatus = "MATCHED"
	RunStatusMismatch RunStatus = "MISMATCH"
	RunStatusFailed   RunStatus = "FAILED"
)

type EntityType string

const (
	EntityBalance  EntityType = "BALANCE"
	EntityPosition EntityType = "POSITION"
	EntityOrder    EntityType = "ORDER"
	EntityFill     EntityType = "FILL"
	EntityMargin   EntityType = "MARGIN"
)

type ItemResult string

const (
	ResultMatch            ItemResult = "MATCH"
	ResultMismatch         ItemResult = "MISMATCH"
	ResultMissingInternal  ItemResult = "MISSING_INTERNAL"
	ResultMissingExternal  ItemResult = "MISSING_EXTERNAL"
	ResultUnresolved       ItemResult = "UNRESOLVED"
)

type ReconciliationRun struct {
	ID             uuid.UUID       `json:"id" db:"id"`
	TenantID       uuid.UUID       `json:"tenant_id" db:"tenant_id"`
	VenueAccountID uuid.UUID       `json:"venue_account_id" db:"venue_account_id"`
	TriggerSource  TriggerSource   `json:"trigger_source" db:"trigger_source"`
	Status         RunStatus       `json:"status" db:"status"`
	StartedAt      time.Time       `json:"started_at" db:"started_at"`
	CompletedAt    *time.Time      `json:"completed_at,omitempty" db:"completed_at"`
	Summary        json.RawMessage `json:"summary" db:"summary"`
}

type ReconciliationItem struct {
	ID            uuid.UUID       `json:"id" db:"id"`
	RunID         uuid.UUID       `json:"run_id" db:"run_id"`
	EntityType    EntityType      `json:"entity_type" db:"entity_type"`
	EntityID      *uuid.UUID      `json:"entity_id,omitempty" db:"entity_id"`
	Result        ItemResult      `json:"result" db:"result"`
	InternalState json.RawMessage `json:"internal_state" db:"internal_state"`
	ExternalState json.RawMessage `json:"external_state" db:"external_state"`
	Diff          json.RawMessage `json:"diff,omitempty" db:"diff"`
	CreatedAt     time.Time       `json:"created_at" db:"created_at"`
}

type BalanceState struct {
	Asset    string  `json:"asset"`
	Amount   float64 `json:"amount"`
	Reserved float64 `json:"reserved"`
}

type PositionState struct {
	Symbol   string  `json:"symbol"`
	Side     string  `json:"side"`
	Quantity float64 `json:"quantity"`
	AvgPrice float64 `json:"avg_price"`
}

type OrderState struct {
	OrderID    string  `json:"order_id"`
	Symbol     string  `json:"symbol"`
	Side       string  `json:"side"`
	Quantity   float64 `json:"quantity"`
	Price      float64 `json:"price"`
	Status     string  `json:"status"`
	FilledQty  float64 `json:"filled_qty"`
}

type MarginState struct {
	TotalEquity    float64 `json:"total_equity"`
	UsedMargin     float64 `json:"used_margin"`
	AvailableMargin float64 `json:"available_margin"`
	MarginRatio    float64 `json:"margin_ratio"`
}

type CreateRunRequest struct {
	VenueAccountID uuid.UUID     `json:"venue_account_id" binding:"required"`
	TriggerSource  TriggerSource `json:"trigger_source" binding:"required,oneof=STARTUP RECONNECT SCHEDULED MANUAL EXECUTION_RECOVERY"`
}

type RunSummary struct {
	TotalItems   int `json:"total_items"`
	Matched      int `json:"matched"`
	Mismatched   int `json:"mismatched"`
	MissingInt   int `json:"missing_internal"`
	MissingExt   int `json:"missing_external"`
}
