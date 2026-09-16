package risk

import (
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

var (
	ErrPolicyNotFound         = domain.NewError(domain.ErrCodeRiskPositionLimit, "risk policy not found")
	ErrPolicyDisabled         = domain.NewError(domain.ErrCodeRiskNotionalLimit, "risk policy disabled")
	ErrKillSwitchActive       = domain.NewError(domain.ErrCodeRiskKillSwitch, "kill switch is active")
	ErrPositionLimitExceeded  = domain.NewError(domain.ErrCodeRiskPositionLimit, "position limit exceeded")
	ErrNotionalLimitExceeded  = domain.NewError(domain.ErrCodeRiskNotionalLimit, "notional limit exceeded")
	ErrLeverageLimitExceeded  = domain.NewError(domain.ErrCodeRiskGlobalLimit, "leverage limit exceeded")
	ErrInsufficientBalance    = domain.NewError(domain.ErrCodeRiskAllocationExhausted, "insufficient balance")
	ErrMarketUnhealthy        = domain.NewError(domain.ErrCodeRiskMarketStale, "market unhealthy")
	ErrVenueUnhealthy         = domain.NewError(domain.ErrCodeRiskVenueUnhealthy, "venue unhealthy")
	ErrConcentrationExceeded  = domain.NewError(domain.ErrCodeRiskConcentration, "concentration limit exceeded")
)
