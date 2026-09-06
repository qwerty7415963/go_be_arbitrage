package orderbook

import "errors"

var (
	ErrBookNotFound      = errors.New("order book not found")
	ErrSequenceMismatch  = errors.New("sequence mismatch")
	ErrBookNotHealthy    = errors.New("order book not healthy")
	ErrBookStale         = errors.New("order book stale")
	ErrBookDesynced      = errors.New("order book desynced")
	ErrBookResyncing     = errors.New("order book resyncing")
	ErrBookDisconnected  = errors.New("order book disconnected")
	ErrInvalidDepth      = errors.New("invalid depth")
	ErrNoBids            = errors.New("no bids in order book")
	ErrNoAsks            = errors.New("no asks in order book")
	ErrEmptyBook         = errors.New("order book is empty")
	ErrSnapshotRequired  = errors.New("snapshot required before deltas")
	ErrDeltaTooOld       = errors.New("delta sequence too old")
)
