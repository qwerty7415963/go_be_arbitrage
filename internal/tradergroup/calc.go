package tradergroup

import (
	"math"
	"sort"
	"time"
)

// TWREquityPoint is one day's aggregated equity across the wallets in a
// group. Start/End are sums over wallets that have both values; Valid reports
// whether the day can contribute to the TWR chain.
type TWREquityPoint struct {
	Date  time.Time
	Start float64
	End   float64
	Valid bool
}

// TWRChain computes the time-weighted return over a chain of daily equity
// points: product(1 + r_t) - 1, where r_t = end/start - 1. Days with
// start <= 0 are skipped. Returns nil when no valid day exists.
func TWRChain(points []TWREquityPoint) *float64 {
	product := 1.0
	has := false
	for _, p := range points {
		if !p.Valid || p.Start <= 0 {
			continue
		}
		r := p.End/p.Start - 1.0
		product *= (1.0 + r)
		has = true
	}
	if !has {
		return nil
	}
	v := product - 1.0
	return &v
}

// MaxDrawdownPct computes the peak-to-trough drawdown percentage on an
// roi_index series (rebased to 100). Returns nil for an empty series.
func MaxDrawdownPct(index []float64) *float64 {
	if len(index) == 0 {
		return nil
	}
	peak := index[0]
	maxDD := 0.0
	has := false
	for _, v := range index {
		if v > peak {
			peak = v
		}
		if peak > 0 {
			dd := (peak - v) / peak * 100.0
			if !has || dd > maxDD {
				maxDD = dd
				has = true
			}
		}
	}
	if !has {
		return nil
	}
	return &maxDD
}

// DeriveGroupStatus derives a group's data_status from member counts.
func DeriveGroupStatus(memberCount, membersWithData int, allReady, anyStale bool) string {
	if memberCount == 0 {
		return "empty"
	}
	if membersWithData == 0 {
		return "error"
	}
	if anyStale {
		return "stale"
	}
	if membersWithData < memberCount {
		return "partial"
	}
	if !allReady {
		return "partial"
	}
	return "ready"
}

// SortGroupListItems sorts group list items with is_favorite DESC first, then
// the requested sort field/direction, then name ASC tiebreak.
func SortGroupListItems(items []*GroupListItem, sortField, dir string) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].IsFavorite != items[j].IsFavorite {
			return items[i].IsFavorite
		}
		less := groupFieldLess(items[i], items[j], sortField)
		if dir == "desc" {
			less = groupFieldLess(items[j], items[i], sortField)
		}
		if groupFieldEqual(items[i], items[j], sortField) {
			return items[i].Name < items[j].Name
		}
		return less
	})
}

func groupFieldLess(a, b *GroupListItem, field string) bool {
	switch field {
	case "name":
		return a.Name < b.Name
	case "account_value":
		return floatPtrLess(a.AccountValue, b.AccountValue)
	case "unrealized_pnl":
		return floatPtrLess(a.UnrealizedPnL, b.UnrealizedPnL)
	case "roi":
		return floatPtrLess(a.ROI, b.ROI)
	case "volume":
		return floatPtrLess(a.Volume, b.Volume)
	case "open_positions":
		return a.OpenPositions < b.OpenPositions
	case "win_rate":
		return floatPtrLess(a.WinRate, b.WinRate)
	case "updated_at":
		return a.UpdatedAt.Before(b.UpdatedAt)
	case "created_at":
		// GroupListItem has no created_at; fall back to updated_at.
		return a.UpdatedAt.Before(b.UpdatedAt)
	default:
		return a.Name < b.Name
	}
}

func groupFieldEqual(a, b *GroupListItem, field string) bool {
	switch field {
	case "name":
		return a.Name == b.Name
	case "account_value":
		return floatPtrEqual(a.AccountValue, b.AccountValue)
	case "unrealized_pnl":
		return floatPtrEqual(a.UnrealizedPnL, b.UnrealizedPnL)
	case "roi":
		return floatPtrEqual(a.ROI, b.ROI)
	case "volume":
		return floatPtrEqual(a.Volume, b.Volume)
	case "open_positions":
		return a.OpenPositions == b.OpenPositions
	case "win_rate":
		return floatPtrEqual(a.WinRate, b.WinRate)
	case "updated_at":
		return a.UpdatedAt.Equal(b.UpdatedAt)
	default:
		return false
	}
}

func floatPtrLess(a, b *float64) bool {
	if a == nil && b == nil {
		return false
	}
	if a == nil {
		return false // nulls last
	}
	if b == nil {
		return true
	}
	return *a < *b
}

func floatPtrEqual(a, b *float64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// WinRate returns 100*... fraction: wins/(wins+losses) (breakeven excluded).
// Returns nil when denominator is 0.
func WinRate(wins, losses int64) *float64 {
	if wins+losses == 0 {
		return nil
	}
	v := float64(wins) / float64(wins+losses)
	return &v
}

// ProfitFactor returns grossProfit/abs(grossLoss). Returns nil when lossCount==0
// or tradeCount<10 (min sample 10).
func ProfitFactor(grossProfit, grossLoss float64, lossCount, tradeCount int64) *float64 {
	if lossCount == 0 || tradeCount < 10 {
		return nil
	}
	if grossLoss == 0 {
		return nil
	}
	v := grossProfit / math.Abs(grossLoss)
	return &v
}
