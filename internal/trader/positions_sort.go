package trader

import (
	"sort"
	"strings"
)

func positionFloat(p Position, sort string) *float64 {
	switch sort {
	case "entry_price":
		return p.EntryPrice
	case "mark_price":
		return p.MarkPrice
	case "position_value":
		return p.PositionValue
	case "unrealized_pnl":
		return p.UnrealizedPnl
	case "return_on_equity":
		return p.ReturnOnEquity
	case "leverage":
		return p.Leverage
	}
	return nil
}

// sortPositions orders the snapshot server-side (§1.1). Null numerics sort
// last regardless of dir; tiebreak coin ASC for stability.
func sortPositions(positions []Position, sortKey, dir string) {
	desc := dir == "desc"
	sort.SliceStable(positions, func(i, j int) bool {
		a, b := positions[i], positions[j]
		switch sortKey {
		case "coin":
			if a.Coin == b.Coin {
				return false
			}
			if desc {
				return a.Coin > b.Coin
			}
			return a.Coin < b.Coin
		case "size":
			if a.Size == b.Size {
				return a.Coin < b.Coin
			}
			if desc {
				return a.Size > b.Size
			}
			return a.Size < b.Size
		default:
			av := positionFloat(a, sortKey)
			bv := positionFloat(b, sortKey)
			if av == nil && bv == nil {
				return a.Coin < b.Coin
			}
			if av == nil {
				return false
			}
			if bv == nil {
				return true
			}
			if *av == *bv {
				return a.Coin < b.Coin
			}
			if desc {
				return *av > *bv
			}
			return *av < *bv
		}
	})
}

func normalizeSideForOrder(side string) string {
	s := strings.ToUpper(strings.TrimSpace(side))
	switch s {
	case "B", "BID", "BUY", "BID_SIDE":
		return "BUY"
	case "A", "ASK", "SELL", "ASK_SIDE":
		return "SELL"
	}
	return s
}
