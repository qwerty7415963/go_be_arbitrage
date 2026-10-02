package trader

import (
	"testing"
	"time"
)

// EQ-U-02: equity aggregation (BE-021: start/end/peak/daily-return).
func TestAggregateEquityDaily(t *testing.T) {
	d1 := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	pts := []EquityPoint{
		{Time: d1.Add(2 * time.Hour), Value: 100},
		{Time: d1.Add(20 * time.Hour), Value: 90},
		{Time: d1.Add(10 * time.Hour), Value: 120},
		{Time: d1.Add(25 * time.Hour), Value: 200},
	}
	days := AggregateEquityDaily(pts)
	if len(days) != 2 {
		t.Fatalf("two UTC days: %v", days)
	}
	d := days["2026-09-30"]
	if d.StartEquity == nil || *d.StartEquity != 100 || d.EndEquity == nil || *d.EndEquity != 90 {
		t.Errorf("start/end: %+v", d)
	}
	if d.PeakEquity == nil || *d.PeakEquity != 120 {
		t.Errorf("peak: %+v", d)
	}
	if d.DailyReturn == nil || *d.DailyReturn < -0.101 || *d.DailyReturn > -0.099 {
		t.Errorf("return 90/100-1: %+v", d.DailyReturn)
	}
}

// EQ-U-03: drawdown incl. the spec's canonical example (BE-022).
func TestMaxDrawdownPct(t *testing.T) {
	if got := MaxDrawdownPct([]float64{100, 120, 90}); got == nil || *got != 25.0 {
		t.Errorf("100->120->90 must be 25%%, got %+v", got)
	}
	if got := MaxDrawdownPct([]float64{100, 110, 130}); got == nil || *got != 0 {
		t.Errorf("monotonic must be 0, got %+v", got)
	}
	if MaxDrawdownPct(nil) != nil {
		t.Error("empty curve must be nil")
	}
	// Zero peak never divides by zero.
	if got := MaxDrawdownPct([]float64{0, 0}); got == nil {
		t.Error("zero curve must not be nil")
	}
}
