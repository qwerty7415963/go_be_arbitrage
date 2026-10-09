package trader

import (
	"context"
	"strings"
	"time"

	"github.com/qwerty7415963/go_be_arbitrage/internal/hyperliquid"
)

// Position is one open position (venue-agnostic). Nullable numerics mirror
// the clearinghouseState decimal strings (nil when absent/unparseable).
type Position struct {
	Coin             string
	Side             string // "LONG" | "SHORT"
	Size             float64
	EntryPrice       *float64
	MarkPrice        *float64
	PositionValue    *float64
	UnrealizedPnl    *float64
	ReturnOnEquity   *float64
	LiquidationPrice *float64
	Leverage         *float64
	MaxLeverage      *float64
	MarginUsed       *float64
}

// PositionSummary is the account-level margin summary (one row per wallet).
type PositionSummary struct {
	AccountValue    *float64
	TotalNtlPos     *float64
	TotalMarginUsed *float64
	AsOf            time.Time
}

// PositionSnapshot is one clearinghouseState poll result.
type PositionSnapshot struct {
	Positions       []Position
	AccountValue    *float64
	TotalNtlPos     *float64
	TotalMarginUsed *float64
	AsOf            time.Time
}

// PositionFetcher is the venue positions seam (clearinghouseState).
type PositionFetcher interface {
	FetchPositions(ctx context.Context, address string) (*PositionSnapshot, error)
}

// HLPositionAdapter maps Hyperliquid clearinghouseState onto positions.
type HLPositionAdapter struct {
	C *hyperliquid.Client
}

var _ PositionFetcher = HLPositionAdapter{}

func parseOpt(s hyperliquid.DecimalString) *float64 {
	t := strings.TrimSpace(string(s))
	if t == "" {
		return nil
	}
	f, err := s.Float()
	if err != nil {
		return nil
	}
	return &f
}

// MapClearinghouseToSnapshot maps a clearinghouseState payload onto the
// venue-agnostic snapshot (shared by the sync adapter and the live read path).
// Side from szi sign (>0 LONG else SHORT), size = |szi|; rows with
// unparseable/zero szi are skipped. MarkPrice is derived as
// positionValue/|szi| (upstream has no per-position markPx); nil when
// positionValue is absent or size is zero.
func MapClearinghouseToSnapshot(state *hyperliquid.ClearinghouseState, now time.Time) *PositionSnapshot {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	snap := &PositionSnapshot{AsOf: now.UTC(), Positions: []Position{}}
	if state == nil {
		return snap
	}
	snap.AccountValue = parseOpt(state.MarginSummary.AccountValue)
	snap.TotalNtlPos = parseOpt(state.MarginSummary.TotalNtlPos)
	snap.TotalMarginUsed = parseOpt(state.MarginSummary.TotalMarginUsed)
	for _, ap := range state.AssetPositions {
		p := ap.Position
		szi, err := p.Szi.Float()
		if err != nil || szi == 0 {
			continue
		}
		coin := strings.ToUpper(strings.TrimSpace(p.Coin))
		if coin == "" {
			continue
		}
		side := "LONG"
		if szi < 0 {
			side = "SHORT"
		}
		size := abs(szi)
		pos := Position{Coin: coin, Side: side, Size: size}
		pos.EntryPrice = parseOpt(p.EntryPx)
		pos.PositionValue = parseOpt(p.PositionValue)
		pos.UnrealizedPnl = parseOpt(p.UnrealizedPnl)
		pos.ReturnOnEquity = parseOpt(p.ReturnOnEquity)
		pos.LiquidationPrice = parseOpt(p.LiquidationPx)
		pos.Leverage = parseOpt(p.Leverage.Value)
		pos.MaxLeverage = parseOpt(p.MaxLeverage)
		pos.MarginUsed = parseOpt(p.MarginUsed)
		if pos.PositionValue != nil && size > 0 {
			mark := abs(*pos.PositionValue) / size
			pos.MarkPrice = &mark
		}
		snap.Positions = append(snap.Positions, pos)
	}
	return snap
}

// FetchPositions calls clearinghouseState and maps decimal strings.
func (a HLPositionAdapter) FetchPositions(ctx context.Context, address string) (*PositionSnapshot, error) {
	state, err := a.C.FetchClearinghouseState(ctx, address)
	if err != nil {
		return nil, err
	}
	return MapClearinghouseToSnapshot(state, time.Now().UTC()), nil
}
