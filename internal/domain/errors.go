package domain

import (
	"errors"
	"fmt"
	"net/http"
)

type ErrorCode string

const (
	// AUTH — Authentication & Authorization (001-099)
	ErrCodeAuthInvalidCredentials ErrorCode = "AUTH-001"
	ErrCodeAuthTokenExpired      ErrorCode = "AUTH-002"
	ErrCodeAuthTokenInvalid      ErrorCode = "AUTH-003"
	ErrCodeAuthRefreshRevoked    ErrorCode = "AUTH-004"
	ErrCodeAuthForbidden         ErrorCode = "AUTH-005"
	ErrCodeAuthDisabled          ErrorCode = "AUTH-006"
	ErrCodeAuthRateLimited       ErrorCode = "AUTH-007"
	ErrCodeAuthInvalidAPIKey     ErrorCode = "AUTH-008"
	ErrCodeAuthAPIKeyExpired     ErrorCode = "AUTH-009"
	ErrCodeAuthSessionLimit      ErrorCode = "AUTH-010"

	// EXCHANGE — Exchange Adapter (101-199)
	ErrCodeExchangeConnectionFailed ErrorCode = "EXCHANGE-101"
	ErrCodeExchangeAuthFailed       ErrorCode = "EXCHANGE-102"
	ErrCodeExchangeRateLimited      ErrorCode = "EXCHANGE-103"
	ErrCodeExchangeWSDisconnected   ErrorCode = "EXCHANGE-104"
	ErrCodeExchangeInvalidResponse  ErrorCode = "EXCHANGE-105"
	ErrCodeExchangeSymbolNotFound   ErrorCode = "EXCHANGE-106"
	ErrCodeExchangeMarketUnavailable ErrorCode = "EXCHANGE-107"
	ErrCodeExchangeTimeout          ErrorCode = "EXCHANGE-108"
	ErrCodeExchangeIPBlacklisted    ErrorCode = "EXCHANGE-109"
	ErrCodeExchangeMaintenance      ErrorCode = "EXCHANGE-110"

	// ORDER — Order Management (201-299)
	ErrCodeOrderCreationFailed  ErrorCode = "ORDER-201"
	ErrCodeOrderNotFound        ErrorCode = "ORDER-202"
	ErrCodeOrderAlreadyFilled   ErrorCode = "ORDER-203"
	ErrCodeOrderCancelled       ErrorCode = "ORDER-204"
	ErrCodeOrderInsufficientBal ErrorCode = "ORDER-205"
	ErrCodeOrderRejected        ErrorCode = "ORDER-206"
	ErrCodeOrderPriceOutOfRange ErrorCode = "ORDER-207"
	ErrCodeOrderQuantityInvalid ErrorCode = "ORDER-208"
	ErrCodeOrderDuplicateID     ErrorCode = "ORDER-209"
	ErrCodeOrderCancelFailed    ErrorCode = "ORDER-210"

	// RISK — Risk Engine (301-399)
	ErrCodeRiskPositionLimit    ErrorCode = "RISK-301"
	ErrCodeRiskNotionalLimit    ErrorCode = "RISK-302"
	ErrCodeRiskConcentration    ErrorCode = "RISK-303"
	ErrCodeRiskKillSwitch       ErrorCode = "RISK-304"
	ErrCodeRiskAllocationExhausted ErrorCode = "RISK-305"
	ErrCodeRiskGlobalLimit      ErrorCode = "RISK-306"
	ErrCodeRiskMarketStale      ErrorCode = "RISK-307"
	ErrCodeRiskVenueUnhealthy   ErrorCode = "RISK-308"
	ErrCodeRiskExecutionVetoed  ErrorCode = "RISK-309"
	ErrCodeRiskHedgeRequired    ErrorCode = "RISK-310"

	// STRATEGY — Strategy Engine (401-499)
	ErrCodeStrategyNotFound    ErrorCode = "STRATEGY-401"
	ErrCodeStrategyDisabled    ErrorCode = "STRATEGY-402"
	ErrCodeStrategyInvalidConf ErrorCode = "STRATEGY-403"
	ErrCodeStrategyExpired     ErrorCode = "STRATEGY-404"
	ErrCodeStrategyInsufficientEdge ErrorCode = "STRATEGY-405"
	ErrCodeStrategyUniverseEmpty    ErrorCode = "STRATEGY-406"
	ErrCodeStrategyModeNotAllowed   ErrorCode = "STRATEGY-407"
	ErrCodeStrategyConfMismatch     ErrorCode = "STRATEGY-408"
	ErrCodeStrategyPaused           ErrorCode = "STRATEGY-409"
	ErrCodeStrategyCalculationError ErrorCode = "STRATEGY-410"

	// MARKET — Market Data (501-599)
	ErrCodeMarketUnavailable     ErrorCode = "MARKET-501"
	ErrCodeMarketBookStale       ErrorCode = "MARKET-502"
	ErrCodeMarketSequenceGap     ErrorCode = "MARKET-503"
	ErrCodeMarketSyncInProgress  ErrorCode = "MARKET-504"
	ErrCodeMarketNotSubscribed   ErrorCode = "MARKET-505"
	ErrCodeMarketFundingMissing  ErrorCode = "MARKET-506"
	ErrCodeMarketTickerUnavailable ErrorCode = "MARKET-507"
	ErrCodeMarketDepthInsufficient ErrorCode = "MARKET-508"
	ErrCodeMarketDuplicateEvent  ErrorCode = "MARKET-509"
	ErrCodeMarketEventOutOfOrder ErrorCode = "MARKET-510"

	// CONFIG — Configuration (601-699)
	ErrCodeConfigNotFound     ErrorCode = "CONFIG-601"
	ErrCodeConfigInvalid      ErrorCode = "CONFIG-602"
	ErrCodeConfigRequired     ErrorCode = "CONFIG-603"
	ErrCodeConfigOutOfRange   ErrorCode = "CONFIG-604"
	ErrCodeConfigLocked       ErrorCode = "CONFIG-605"
	ErrCodeConfigVersionMismatch ErrorCode = "CONFIG-606"
	ErrCodeConfigActivationFailed ErrorCode = "CONFIG-607"
	ErrCodeConfigRollbackFailed   ErrorCode = "CONFIG-608"

	// SYNC — Reconciliation (701-799)
	ErrCodeSyncFailed         ErrorCode = "SYNC-701"
	ErrCodeSyncBalanceMismatch    ErrorCode = "SYNC-702"
	ErrCodeSyncPositionMismatch   ErrorCode = "SYNC-703"
	ErrCodeSyncOrderMismatch      ErrorCode = "SYNC-704"
	ErrCodeSyncFillMismatch       ErrorCode = "SYNC-705"
	ErrCodeSyncExchangeUnreachable ErrorCode = "SYNC-706"
	ErrCodeSyncPartialSync        ErrorCode = "SYNC-707"
	ErrCodeSyncResumeBlocked      ErrorCode = "SYNC-708"
	ErrCodeSyncStaleState         ErrorCode = "SYNC-709"
	ErrCodeSyncManualRequired     ErrorCode = "SYNC-710"

	// COMMON — Cross-cutting (901-999)
	ErrCodeInternal    ErrorCode = "COMMON-901"
	ErrCodeValidation  ErrorCode = "COMMON-902"
	ErrCodeNotFound    ErrorCode = "COMMON-903"
	ErrCodeConflict    ErrorCode = "COMMON-904"
	ErrCodeRateLimited ErrorCode = "COMMON-905"
	ErrCodeTimeout     ErrorCode = "COMMON-906"
	ErrCodeTooLarge    ErrorCode = "COMMON-907"
	ErrCodeMethodNotAllowed ErrorCode = "COMMON-908"
)

type AppError struct {
	Code       ErrorCode   `json:"code"`
	Message    string      `json:"message"`
	Details    interface{} `json:"details,omitempty"`
	StatusCode int         `json:"-"`
	Err        error       `json:"-"`
}

func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *AppError) Unwrap() error {
	return e.Err
}

func (e *AppError) WithDetails(details interface{}) *AppError {
	return &AppError{
		Code:       e.Code,
		Message:    e.Message,
		Details:    details,
		StatusCode: e.StatusCode,
		Err:        e.Err,
	}
}

func (e *AppError) WithErr(err error) *AppError {
	return &AppError{
		Code:       e.Code,
		Message:    e.Message,
		Details:    e.Details,
		StatusCode: e.StatusCode,
		Err:        err,
	}
}

func ServiceFromCode(code ErrorCode) string {
	for i, c := range code {
		if c == '-' {
			return string(code[:i])
		}
	}
	return "UNKNOWN"
}

var errorStatusMap = map[ErrorCode]int{
	ErrCodeAuthInvalidCredentials: http.StatusBadRequest,
	ErrCodeAuthTokenExpired:       http.StatusUnauthorized,
	ErrCodeAuthTokenInvalid:       http.StatusUnauthorized,
	ErrCodeAuthRefreshRevoked:     http.StatusUnauthorized,
	ErrCodeAuthForbidden:          http.StatusForbidden,
	ErrCodeAuthDisabled:           http.StatusForbidden,
	ErrCodeAuthRateLimited:        http.StatusTooManyRequests,
	ErrCodeAuthInvalidAPIKey:      http.StatusUnauthorized,
	ErrCodeAuthAPIKeyExpired:      http.StatusUnauthorized,
	ErrCodeAuthSessionLimit:       http.StatusForbidden,

	ErrCodeExchangeConnectionFailed:   http.StatusBadGateway,
	ErrCodeExchangeAuthFailed:         http.StatusBadGateway,
	ErrCodeExchangeRateLimited:        http.StatusTooManyRequests,
	ErrCodeExchangeWSDisconnected:     http.StatusBadGateway,
	ErrCodeExchangeInvalidResponse:    http.StatusBadGateway,
	ErrCodeExchangeSymbolNotFound:     http.StatusNotFound,
	ErrCodeExchangeMarketUnavailable:  http.StatusBadGateway,
	ErrCodeExchangeTimeout:            http.StatusGatewayTimeout,
	ErrCodeExchangeIPBlacklisted:      http.StatusForbidden,
	ErrCodeExchangeMaintenance:        http.StatusServiceUnavailable,

	ErrCodeOrderCreationFailed:  http.StatusBadRequest,
	ErrCodeOrderNotFound:        http.StatusNotFound,
	ErrCodeOrderAlreadyFilled:   http.StatusConflict,
	ErrCodeOrderCancelled:       http.StatusConflict,
	ErrCodeOrderInsufficientBal: http.StatusBadRequest,
	ErrCodeOrderRejected:        http.StatusUnprocessableEntity,
	ErrCodeOrderPriceOutOfRange: http.StatusBadRequest,
	ErrCodeOrderQuantityInvalid: http.StatusBadRequest,
	ErrCodeOrderDuplicateID:     http.StatusConflict,
	ErrCodeOrderCancelFailed:    http.StatusInternalServerError,

	ErrCodeRiskPositionLimit:        http.StatusUnprocessableEntity,
	ErrCodeRiskNotionalLimit:        http.StatusUnprocessableEntity,
	ErrCodeRiskConcentration:        http.StatusUnprocessableEntity,
	ErrCodeRiskKillSwitch:           http.StatusUnprocessableEntity,
	ErrCodeRiskAllocationExhausted:  http.StatusUnprocessableEntity,
	ErrCodeRiskGlobalLimit:          http.StatusUnprocessableEntity,
	ErrCodeRiskMarketStale:          http.StatusUnprocessableEntity,
	ErrCodeRiskVenueUnhealthy:       http.StatusUnprocessableEntity,
	ErrCodeRiskExecutionVetoed:      http.StatusUnprocessableEntity,
	ErrCodeRiskHedgeRequired:        http.StatusUnprocessableEntity,

	ErrCodeStrategyNotFound:          http.StatusNotFound,
	ErrCodeStrategyDisabled:          http.StatusUnprocessableEntity,
	ErrCodeStrategyInvalidConf:       http.StatusBadRequest,
	ErrCodeStrategyExpired:           http.StatusUnprocessableEntity,
	ErrCodeStrategyInsufficientEdge:  http.StatusUnprocessableEntity,
	ErrCodeStrategyUniverseEmpty:     http.StatusUnprocessableEntity,
	ErrCodeStrategyModeNotAllowed:    http.StatusUnprocessableEntity,
	ErrCodeStrategyConfMismatch:      http.StatusBadRequest,
	ErrCodeStrategyPaused:            http.StatusUnprocessableEntity,
	ErrCodeStrategyCalculationError:  http.StatusInternalServerError,

	ErrCodeMarketUnavailable:        http.StatusServiceUnavailable,
	ErrCodeMarketBookStale:          http.StatusServiceUnavailable,
	ErrCodeMarketSequenceGap:        http.StatusServiceUnavailable,
	ErrCodeMarketSyncInProgress:     http.StatusServiceUnavailable,
	ErrCodeMarketNotSubscribed:      http.StatusBadRequest,
	ErrCodeMarketFundingMissing:     http.StatusServiceUnavailable,
	ErrCodeMarketTickerUnavailable:  http.StatusServiceUnavailable,
	ErrCodeMarketDepthInsufficient:  http.StatusServiceUnavailable,
	ErrCodeMarketDuplicateEvent:     http.StatusConflict,
	ErrCodeMarketEventOutOfOrder:    http.StatusConflict,

	ErrCodeConfigNotFound:           http.StatusNotFound,
	ErrCodeConfigInvalid:            http.StatusBadRequest,
	ErrCodeConfigRequired:           http.StatusBadRequest,
	ErrCodeConfigOutOfRange:         http.StatusBadRequest,
	ErrCodeConfigLocked:             http.StatusConflict,
	ErrCodeConfigVersionMismatch:    http.StatusConflict,
	ErrCodeConfigActivationFailed:   http.StatusInternalServerError,
	ErrCodeConfigRollbackFailed:     http.StatusInternalServerError,

	ErrCodeSyncFailed:               http.StatusInternalServerError,
	ErrCodeSyncBalanceMismatch:      http.StatusUnprocessableEntity,
	ErrCodeSyncPositionMismatch:     http.StatusUnprocessableEntity,
	ErrCodeSyncOrderMismatch:        http.StatusUnprocessableEntity,
	ErrCodeSyncFillMismatch:         http.StatusUnprocessableEntity,
	ErrCodeSyncExchangeUnreachable:  http.StatusBadGateway,
	ErrCodeSyncPartialSync:          http.StatusPartialContent,
	ErrCodeSyncResumeBlocked:        http.StatusUnprocessableEntity,
	ErrCodeSyncStaleState:           http.StatusUnprocessableEntity,
	ErrCodeSyncManualRequired:       http.StatusUnprocessableEntity,

	ErrCodeInternal:           http.StatusInternalServerError,
	ErrCodeValidation:         http.StatusBadRequest,
	ErrCodeNotFound:           http.StatusNotFound,
	ErrCodeConflict:           http.StatusConflict,
	ErrCodeRateLimited:        http.StatusTooManyRequests,
	ErrCodeTimeout:            http.StatusGatewayTimeout,
	ErrCodeTooLarge:           http.StatusRequestEntityTooLarge,
	ErrCodeMethodNotAllowed:   http.StatusMethodNotAllowed,
}

func (e *AppError) StatusCodeFromCode() int {
	if status, ok := errorStatusMap[e.Code]; ok {
		return status
	}
	return http.StatusInternalServerError
}

func NewError(code ErrorCode, message string) *AppError {
	return &AppError{
		Code:       code,
		Message:    message,
		StatusCode: errorStatusMap[code],
	}
}

func NewErrorf(code ErrorCode, format string, args ...interface{}) *AppError {
	return &AppError{
		Code:       code,
		Message:    fmt.Sprintf(format, args...),
		StatusCode: errorStatusMap[code],
	}
}

func WrapError(code ErrorCode, message string, err error) *AppError {
	return &AppError{
		Code:       code,
		Message:    message,
		StatusCode: errorStatusMap[code],
		Err:        err,
	}
}

func IsAppError(err error) bool {
	var appErr *AppError
	return errors.As(err, &appErr)
}

func GetAppError(err error) *AppError {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr
	}
	return NewError(ErrCodeInternal, "internal server error").WithErr(err)
}
