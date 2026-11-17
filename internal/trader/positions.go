package trader

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
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
	TpPxs            []float64
	SlPxs            []float64
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

// openOrdersDexClient is the seam used by the open-orders multi-dex aggregation.
type openOrdersDexClient interface {
	FetchOpenOrders(ctx context.Context, address string, dex ...string) ([]hyperliquid.OpenOrder, error)
}

// fetchOpenOrdersAllDexes aggregates open orders across the default dex and the
// configured perp dexes. Per-dex failures are non-fatal; an error is returned
// only when every dex fails. Results are merged, deduplicated by oid, and sorted
// by timestamp descending.
func fetchOpenOrdersAllDexes(ctx context.Context, client any, addr string, dexes []string) ([]OrderDTO, error) {
	c, ok := client.(openOrdersDexClient)
	if !ok {
		return nil, fmt.Errorf("client does not implement open orders dex seam")
	}
	if dexes == nil {
		dexes = []string{}
	}
	jobs := make([]string, 0, 1+len(dexes))
	jobs = append(jobs, "")
	jobs = append(jobs, dexes...)

	results := make([][]OrderDTO, len(jobs))
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
			var raw []hyperliquid.OpenOrder
			var err error
			if d == "" {
				raw, err = c.FetchOpenOrders(ctx, addr)
			} else {
				raw, err = c.FetchOpenOrders(ctx, addr, d)
			}
			if err != nil {
				errs[i] = err
				return
			}
			results[i] = mapOpenOrders(raw, false)
		}(i, d)
	}
	wg.Wait()

	success := 0
	seen := make(map[int64]struct{})
	merged := make([]OrderDTO, 0)
	for i, rows := range results {
		if errs[i] != nil && rows == nil {
			// allow empty successful
		}
		if rows == nil && errs[i] != nil {
			continue
		}
		// count as success if no error
		if errs[i] == nil {
			success++
		}
		for _, o := range rows {
			if o.Oid != 0 {
				if _, ok := seen[o.Oid]; ok {
					continue
				}
				seen[o.Oid] = struct{}{}
			}
			merged = append(merged, o)
		}
	}
	if success == 0 {
		for _, e := range errs {
			if e != nil {
				return nil, e
			}
		}
		return nil, fmt.Errorf("all dexes failed")
	}
	// sort timestamp DESC
	for i := 1; i < len(merged); i++ {
		for j := i; j > 0; j-- {
			if merged[j].Timestamp.After(merged[j-1].Timestamp) {
				merged[j], merged[j-1] = merged[j-1], merged[j]
			} else {
				break
			}
		}
	}
	if merged == nil {
		merged = []OrderDTO{}
	}
	return merged, nil
}
func isClosingSide(posSide, orderSide string) bool {
	ps := strings.ToUpper(strings.TrimSpace(posSide))
	os := strings.ToUpper(strings.TrimSpace(orderSide))
	switch ps {
	case "LONG":
		return os == "A" || os == "ASK" || os == "SELL" || os == "SHORT"
	case "SHORT":
		return os == "B" || os == "BID" || os == "BUY" || os == "LONG"
	}
	return false
}

func flattenOrders(orders []hyperliquid.OpenOrder) []hyperliquid.OpenOrder {
	out := make([]hyperliquid.OpenOrder, 0, len(orders))
	var walk func(o hyperliquid.OpenOrder, parent *hyperliquid.OpenOrder)
	walk = func(o hyperliquid.OpenOrder, parent *hyperliquid.OpenOrder) {
		if parent != nil {
			if strings.TrimSpace(o.Coin) == "" {
				o.Coin = parent.Coin
			}
			if strings.TrimSpace(o.Side) == "" {
				o.Side = parent.Side
			}
			if !o.ReduceOnly && parent.ReduceOnly {
				o.ReduceOnly = true
			}
			if strings.TrimSpace(o.TriggerPx) == "" {
				o.TriggerPx = parent.TriggerPx
			}
			if strings.TrimSpace(o.OrderType) == "" {
				o.OrderType = parent.OrderType
			}
			if o.IsTrigger == nil {
				o.IsTrigger = parent.IsTrigger
			}
			if o.IsPositionTpsl == nil {
				o.IsPositionTpsl = parent.IsPositionTpsl
			}
		}
		out = append(out, o)
		for _, ch := range o.Children {
			cc := ch
			walk(cc, &o)
		}
	}
	for _, o := range orders {
		oo := o
		walk(oo, nil)
	}
	if out == nil {
		out = []hyperliquid.OpenOrder{}
	}
	return out
}

func normCoin(v any) string {
	s := ""
	switch val := v.(type) {
	case string:
		s = val
	case hyperliquid.OpenOrder:
		s = val.Coin
	case *hyperliquid.OpenOrder:
		if val != nil {
			s = val.Coin
		}
	}
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, ":"); i >= 0 {
		s = s[i+1:]
	}
	return strings.ToUpper(strings.TrimSpace(s))
}

func parseTrigger(v any) (float64, bool) {
	s := ""
	switch val := v.(type) {
	case string:
		s = val
	case hyperliquid.OpenOrder:
		s = val.TriggerPx
	case *hyperliquid.OpenOrder:
		if val != nil {
			s = val.TriggerPx
		}
	case float64:
		return val, true
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

func isTP(v any) bool {
	ot := ""
	switch val := v.(type) {
	case string:
		ot = val
	case hyperliquid.OpenOrder:
		ot = val.OrderType
	case *hyperliquid.OpenOrder:
		if val != nil {
			ot = val.OrderType
		}
	}
	return strings.Contains(strings.ToLower(ot), "take profit")
}

func isSL(v any) bool {
	ot := ""
	switch val := v.(type) {
	case string:
		ot = val
	case hyperliquid.OpenOrder:
		ot = val.OrderType
	case *hyperliquid.OpenOrder:
		if val != nil {
			ot = val.OrderType
		}
	}
	return strings.Contains(strings.ToLower(ot), "stop")
}

func dedupeFloats(xs []float64) []float64 {
	seen := map[string]struct{}{}
	out := make([]float64, 0, len(xs))
	for _, x := range xs {
		key := strconv.FormatFloat(x, 'f', -1, 64)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, x)
	}
	if out == nil {
		out = []float64{}
	}
	return out
}

func sortTPSL(prices []float64, mark *float64) []float64 {
	arr := prices
	if arr == nil {
		arr = []float64{}
		return arr
	}
	sort.SliceStable(arr, func(i, j int) bool {
		if mark == nil {
			return arr[i] < arr[j]
		}
		di := math.Abs(arr[i] - *mark)
		dj := math.Abs(arr[j] - *mark)
		if di != dj {
			return di < dj
		}
		return arr[i] < arr[j]
	})
	return arr
}

func attachTPSL(positions []Position, orders []hyperliquid.OpenOrder) []Position {
	if positions == nil {
		positions = []Position{}
	}
	out := make([]Position, len(positions))
	copy(out, positions)
	flat := flattenOrders(orders)
	for pi := range out {
		pos := &out[pi]
		if pos.TpPxs == nil {
			pos.TpPxs = []float64{}
		}
		if pos.SlPxs == nil {
			pos.SlPxs = []float64{}
		}
		tp := []float64{}
		sl := []float64{}
		for _, o := range flat {
			if !o.ReduceOnly {
				continue
			}
			trig, ok := parseTrigger(o)
			if !ok || trig <= 0 {
				continue
			}
			if !isClosingSide(pos.Side, o.Side) {
				continue
			}
			if normCoin(o) != pos.Coin {
				continue
			}
			if isTP(o) {
				tp = append(tp, trig)
			} else if isSL(o) {
				sl = append(sl, trig)
			}
		}
		pos.TpPxs = sortTPSL(dedupeFloats(tp), pos.MarkPrice)
		pos.SlPxs = sortTPSL(dedupeFloats(sl), pos.MarkPrice)
		if pos.TpPxs == nil {
			pos.TpPxs = []float64{}
		}
		if pos.SlPxs == nil {
			pos.SlPxs = []float64{}
		}
	}
	return out
}
