package trader

import (
	"context"
	"fmt"
	"strings"
	"sync"
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

// perpDexClient is the seam used by the multi-dex aggregation helpers.
type perpDexClient interface {
	FetchPerpDexs(ctx context.Context) ([]string, error)
	FetchClearinghouseState(ctx context.Context, address string, dex ...string) (*hyperliquid.ClearinghouseState, error)
}

// fetchPositionsAllDexes aggregates open positions across the default dex and
// every named perp dex (HIP-3). Per-dex failures are non-fatal; the result is
// an error only when every dex fails.
func fetchPositionsAllDexes(ctx context.Context, client any, addr string) (*PositionSnapshot, error) {
	c, ok := client.(perpDexClient)
	if !ok {
		// Allow concrete pointer types that satisfy the seam via assertion.
		if pc, ok2 := client.(perpDexClient); ok2 {
			c = pc
		} else {
			return nil, fmt.Errorf("client does not implement perp dex seam")
		}
	}
	dexes, _ := c.FetchPerpDexs(ctx)
	if dexes == nil {
		dexes = []string{}
	}
	return fetchPositionsForDexes(ctx, c, addr, dexes)
}

// fetchPositionsForDexes aggregates for an explicit dex list (empty/nil =>
// default dex only). Exported-ish helper used by the service to reuse a cached
// dex list.
func fetchPositionsForDexes(ctx context.Context, client perpDexClient, addr string, dexes []string) (*PositionSnapshot, error) {
	if client == nil {
		return nil, fmt.Errorf("upstream unavailable")
	}
	if dexes == nil {
		dexes = []string{}
	}
	jobs := make([]string, 0, 1+len(dexes))
	jobs = append(jobs, "")
	jobs = append(jobs, dexes...)

	now := time.Now().UTC()
	results := make([]*PositionSnapshot, len(jobs))
	errs := make([]error, len(jobs))

	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for i, d := range jobs {
		wg.Add(1)
		go func(i int, d string) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				errs[i] = ctx.Err()
				return
			}
			defer func() { <-sem }()
			var st *hyperliquid.ClearinghouseState
			var err error
			if d == "" {
				st, err = client.FetchClearinghouseState(ctx, addr)
			} else {
				st, err = client.FetchClearinghouseState(ctx, addr, d)
			}
			if err != nil {
				errs[i] = err
				return
			}
			if st == nil {
				errs[i] = fmt.Errorf("nil state")
				return
			}
			results[i] = MapClearinghouseToSnapshot(st, now)
		}(i, d)
	}
	wg.Wait()

	success := 0
	snap := &PositionSnapshot{AsOf: now, Positions: []Position{}}
	var avSum, ntlSum, marginSum float64
	var avHas, ntlHas, marginHas bool
	for i, r := range results {
		if r == nil {
			continue
		}
		success++
		snap.Positions = append(snap.Positions, r.Positions...)
		if r.AccountValue != nil {
			avSum += *r.AccountValue
			avHas = true
		}
		if r.TotalNtlPos != nil {
			ntlSum += *r.TotalNtlPos
			ntlHas = true
		}
		if r.TotalMarginUsed != nil {
			marginSum += *r.TotalMarginUsed
			marginHas = true
		}
		_ = errs[i]
	}
	if success == 0 {
		for _, e := range errs {
			if e != nil {
				return nil, e
			}
		}
		return nil, fmt.Errorf("all dexes failed")
	}
	if avHas {
		v := avSum
		snap.AccountValue = &v
	}
	if ntlHas {
		v := ntlSum
		snap.TotalNtlPos = &v
	}
	if marginHas {
		v := marginSum
		snap.TotalMarginUsed = &v
	}
	if snap.Positions == nil {
		snap.Positions = []Position{}
	}
	return snap, nil
}
