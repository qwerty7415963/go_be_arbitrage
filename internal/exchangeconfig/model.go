package exchangeconfig

import (
	"time"

	"github.com/google/uuid"
)

type ExchangeConfigStatus string

const (
	ExchangeConfigStatusActive   ExchangeConfigStatus = "ACTIVE"
	ExchangeConfigStatusDisabled ExchangeConfigStatus = "DISABLED"
)

type ExchangeConfig struct {
	ID            uuid.UUID           `json:"id" db:"id"`
	VenueID       uuid.UUID           `json:"venue_id" db:"venue_id"`
	ExchangeName  string              `json:"exchange_name" db:"exchange_name"`
	RestBaseURL   string              `json:"rest_base_url" db:"rest_base_url"`
	WsURL         string              `json:"ws_url" db:"ws_url"`
	Status        ExchangeConfigStatus `json:"status" db:"status"`
	RateLimitRPM  int                 `json:"rate_limit_rpm" db:"rate_limit_rpm"`
	TimeoutMs     int                 `json:"timeout_ms" db:"timeout_ms"`
	CreatedBy     *uuid.UUID          `json:"created_by" db:"created_by"`
	CreatedAt     time.Time           `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time           `json:"updated_at" db:"updated_at"`
}

type CreateExchangeConfigRequest struct {
	VenueID      uuid.UUID `json:"venue_id" binding:"required"`
	ExchangeName string    `json:"exchange_name" binding:"required"`
	RestBaseURL  string    `json:"rest_base_url" binding:"required"`
	WsURL        string    `json:"ws_url" binding:"required"`
	RateLimitRPM int       `json:"rate_limit_rpm"`
	TimeoutMs    int       `json:"timeout_ms"`
}

type UpdateExchangeConfigRequest struct {
	ExchangeName *string `json:"exchange_name"`
	RestBaseURL  *string `json:"rest_base_url"`
	WsURL        *string `json:"ws_url"`
	Status       *ExchangeConfigStatus `json:"status" binding:"omitempty,oneof=ACTIVE DISABLED"`
	RateLimitRPM *int    `json:"rate_limit_rpm"`
	TimeoutMs    *int    `json:"timeout_ms"`
}
