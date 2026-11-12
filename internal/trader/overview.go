package trader

import (
	"time"
)

// PnLBucketOrder is the canonical display order.
var PnLBucketOrder = []string{
	"extremely_profitable", "very_profitable", "profitable",
	"unprofitable", "very_unprofitable", "rekt",
}

// AccountBucketOrder is the canonical display order.
var AccountBucketOrder = []string{
	"apex", "whale", "large", "medium", "small",
}

// PnLBucketSpec holds min/max for a PnL bucket.
type PnLBucketSpec struct {
	ID  string
	Min *float64
	Max *float64
}

// AccountBucketSpec holds min/max for an account bucket.
type AccountBucketSpec struct {
	ID  string
	Min *float64
	Max *float64
}

func ovFptr(v float64) *float64 { return &v }

var PnLBucketSpecs = []PnLBucketSpec{
	{ID: "extremely_profitable", Min: ovFptr(1000000), Max: nil},
	{ID: "very_profitable", Min: ovFptr(100000), Max: ovFptr(1000000)},
	{ID: "profitable", Min: ovFptr(0), Max: ovFptr(100000)},
	{ID: "unprofitable", Min: ovFptr(-100000), Max: ovFptr(0)},
	{ID: "very_unprofitable", Min: ovFptr(-1000000), Max: ovFptr(-100000)},
	{ID: "rekt", Min: nil, Max: ovFptr(-1000000)},
}

var AccountBucketSpecs = []AccountBucketSpec{
	{ID: "apex", Min: ovFptr(5000000), Max: nil},
	{ID: "whale", Min: ovFptr(1000000), Max: ovFptr(5000000)},
	{ID: "large", Min: ovFptr(100000), Max: ovFptr(1000000)},
	{ID: "medium", Min: ovFptr(10000), Max: ovFptr(100000)},
	{ID: "small", Min: ovFptr(0), Max: ovFptr(10000)},
}

func PnLBucketFor(pnl float64) string {
	if pnl == 0 {
		return ""
	}
	switch {
	case pnl >= 1000000:
		return "extremely_profitable"
	case pnl > 100000:
		return "very_profitable"
	case pnl > 0:
		return "profitable"
	case pnl >= -100000:
		return "unprofitable"
	case pnl > -1000000:
		return "very_unprofitable"
	default:
		return "rekt"
	}
}

func AccountBucketFor(equity float64) string {
	switch {
	case equity >= 5000000:
		return "apex"
	case equity >= 1000000:
		return "whale"
	case equity >= 100000:
		return "large"
	case equity >= 10000:
		return "medium"
	default:
		return "small"
	}
}

func LongRatio(longCount, shortCount int64) float64 {
	total := longCount + shortCount
	if total == 0 {
		return 0
	}
	return float64(longCount) / float64(total)
}

func Sentiment(longCount, shortCount int64) string {
	total := longCount + shortCount
	if total == 0 {
		return "neutral"
	}
	ratio := float64(longCount) / float64(total)
	switch {
	case ratio >= 0.65:
		return "very_bullish"
	case ratio >= 0.55:
		return "bullish"
	case ratio >= 0.52:
		return "slightly_bullish"
	case ratio >= 0.48:
		return "neutral"
	case ratio >= 0.45:
		return "slightly_bearish"
	case ratio >= 0.35:
		return "bearish"
	default:
		return "very_bearish"
	}
}

func ApproxNotional(volume float64, longCount, shortCount int64) (float64, float64) {
	total := longCount + shortCount
	if total == 0 {
		return 0, 0
	}
	long := volume * float64(longCount) / float64(total)
	short := volume * float64(shortCount) / float64(total)
	return long, short
}

// CumulativeSeries builds a cumulative daily series for a bucket over the
// window [start, end] inclusive, carrying forward the last value for missing
// days. daily maps date (truncated UTC day) -> raw sum for that day.
func CumulativeSeries(daily map[time.Time]float64, start, end time.Time) []OverviewPoint {
	out := []OverviewPoint{}
	cur := 0.0
	day := start.UTC().Truncate(24 * time.Hour)
	endDay := end.UTC().Truncate(24 * time.Hour)
	for !day.After(endDay) {
		if v, ok := daily[day]; ok {
			cur += v
		}
		out = append(out, OverviewPoint{
			T: day.UTC().Format("2006-01-02"),
			V: cur,
		})
		day = day.Add(24 * time.Hour)
	}

	return out
}

// SentimentRatio maps a long share (0..1) to a sentiment label.
func SentimentRatio(ratio float64) string {
	switch {
	case ratio >= 0.65:
		return "very_bullish"
	case ratio >= 0.55:
		return "bullish"
	case ratio >= 0.52:
		return "slightly_bullish"
	case ratio >= 0.48:
		return "neutral"
	case ratio >= 0.45:
		return "slightly_bearish"
	case ratio >= 0.35:
		return "bearish"
	default:
		return "very_bearish"
	}
}

// SentimentFloat is Sentiment for notional (or any float) long/short.
func SentimentFloat(long, short float64) string {
	total := long + short
	if total == 0 {
		return "neutral"
	}
	return SentimentRatio(long / total)
}

// ValidBucketID reports whether id is a known bucket for kind.
func ValidBucketID(kind, id string) bool {
	switch kind {
	case "pnl":
		for _, b := range PnLBucketOrder {
			if b == id {
				return true
			}
		}
	case "account":
		for _, b := range AccountBucketOrder {
			if b == id {
				return true
			}
		}
	}
	return false
}
