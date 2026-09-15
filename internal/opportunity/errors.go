package opportunity

import "errors"

var (
	ErrNoOpportunityFound    = errors.New("no opportunity found")
	ErrInsufficientDepth     = errors.New("insufficient order book depth")
	ErrVenueNotHealthy       = errors.New("venue not healthy")
	ErrDataTooStale          = errors.New("market data too stale")
	ErrBelowMinEdge          = errors.New("below minimum edge threshold")
	ErrBelowMinConfidence    = errors.New("below minimum confidence threshold")
	ErrExceedsCapitalLimit   = errors.New("exceeds capital constraints")
	ErrBelowMinTradeSize     = errors.New("below minimum trade size")
	ErrInvalidPrice          = errors.New("invalid price")
	ErrInvalidQuantity       = errors.New("invalid quantity")
	ErrNoFundingData         = errors.New("no funding data available")
	ErrInstrumentNotFound    = errors.New("instrument not found")
	ErrVenueNotFound         = errors.New("venue not found")
	ErrCalculationFailed     = errors.New("opportunity calculation failed")
)
