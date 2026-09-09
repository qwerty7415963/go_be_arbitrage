package venue

import (
	"time"

	"github.com/google/uuid"
)

type VenueType string

const (
	VenueTypeCEX    VenueType = "CEX"
	VenueTypePerpDEX VenueType = "PERP_DEX"
)

type VenueStatus string

const (
	VenueStatusActive   VenueStatus = "ACTIVE"
	VenueStatusDisabled VenueStatus = "DISABLED"
)

type Venue struct {
	ID            uuid.UUID   `json:"id" db:"id"`
	Code          string      `json:"code" db:"code"`
	Name          string      `json:"name" db:"name"`
	VenueType     VenueType   `json:"venue_type" db:"venue_type"`
	ExchangeName  string      `json:"exchange_name" db:"exchange_name"`
	RestBaseURL   string      `json:"rest_base_url" db:"rest_base_url"`
	WsURL         string      `json:"ws_url" db:"ws_url"`
	RateLimitRPM  int         `json:"rate_limit_rpm" db:"rate_limit_rpm"`
	TimeoutMs     int         `json:"timeout_ms" db:"timeout_ms"`
	Status        VenueStatus `json:"status" db:"status"`
	Capabilities  interface{} `json:"capabilities" db:"capabilities"`
	Metadata      interface{} `json:"metadata" db:"metadata"`
	CreatedAt     time.Time   `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time   `json:"updated_at" db:"updated_at"`
}

type Capabilities struct {
	SupportsSpot    bool `json:"supports_spot"`
	SupportsPerp    bool `json:"supports_perp"`
	SupportsFutures bool `json:"supports_futures"`
	HasWS          bool `json:"has_ws"`
	HasREST        bool `json:"has_rest"`
}

type CreateVenueRequest struct {
	Code         string       `json:"code" binding:"required"`
	Name         string       `json:"name" binding:"required"`
	VenueType    VenueType    `json:"venue_type" binding:"required,oneof=CEX PERP_DEX"`
	ExchangeName string       `json:"exchange_name" binding:"required"`
	RestBaseURL  string       `json:"rest_base_url" binding:"required"`
	WsURL        string       `json:"ws_url" binding:"required"`
	RateLimitRPM int          `json:"rate_limit_rpm"`
	TimeoutMs    int          `json:"timeout_ms"`
	Capabilities Capabilities `json:"capabilities"`
}

type UpdateVenueRequest struct {
	Name         *string       `json:"name"`
	ExchangeName *string       `json:"exchange_name"`
	RestBaseURL  *string       `json:"rest_base_url"`
	WsURL        *string       `json:"ws_url"`
	RateLimitRPM *int          `json:"rate_limit_rpm"`
	TimeoutMs    *int          `json:"timeout_ms"`
	Status       *VenueStatus  `json:"status" binding:"omitempty,oneof=ACTIVE DISABLED"`
	Capabilities *Capabilities `json:"capabilities"`
}
