package reconciliation

import "github.com/qwerty7415963/go_be_arbitrage/internal/domain"

var (
	ErrRunNotFound        = domain.NewError(domain.ErrCodeNotFound, "reconciliation run not found")
	ErrRunNotRunning      = domain.NewError(domain.ErrCodeValidation, "reconciliation run is not running")
	ErrInvalidTrigger     = domain.NewError(domain.ErrCodeValidation, "invalid trigger source")
	ErrVenueAccountReq    = domain.NewError(domain.ErrCodeValidation, "venue account ID is required")
	ErrExchangeUnreachable = domain.WrapError(domain.ErrCodeSyncExchangeUnreachable, "exchange unreachable", nil)
	ErrBalanceMismatch    = domain.NewError(domain.ErrCodeSyncBalanceMismatch, "balance mismatch detected")
	ErrPositionMismatch   = domain.NewError(domain.ErrCodeSyncPositionMismatch, "position mismatch detected")
	ErrOrderMismatch      = domain.NewError(domain.ErrCodeSyncOrderMismatch, "order mismatch detected")
	ErrFillMismatch       = domain.NewError(domain.ErrCodeSyncFillMismatch, "fill mismatch detected")
)
