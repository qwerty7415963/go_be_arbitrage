package wallet

import (
	"testing"
	"time"
)

func ts(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

// near compares floats with a small tolerance (float rounding differs
// between compile-time constant folding and runtime division).
func near(a, b float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < 1e-9
}

// SCAN-U-13: normal win/loss fixture → deterministic PnL / volume / ROI /
// win_rate.
func TestComputeMetrics_WinLossFixture_Deterministic(t *testing.T) {
	fills := []MetricFill{
		{PositionID: "p1", Side: "LONG", Timestamp: ts("2026-09-01T10:00:00Z"), Quantity: 2, Price: 100, RealizedPnl: 40},
		{PositionID: "p2", Side: "SHORT", Timestamp: ts("2026-09-02T10:00:00Z"), Quantity: 1, Price: 200, RealizedPnl: -30},
		{PositionID: "p3", Side: "LONG", Timestamp: ts("2026-09-03T10:00:00Z"), Quantity: 3, Price: 50, RealizedPnl: 10},
	}

	m := ComputeMetrics(fills, MetricWindow{})
	if m == nil {
		t.Fatal("expected metrics")
	}

	// PnL = 40 - 30 + 10 = 20
	if m.RealizedPnl == nil || *m.RealizedPnl != 20 {
		t.Errorf("pnl: expected 20, got %v", m.RealizedPnl)
	}
	// volume = 200 + 200 + 150 = 550
	if m.Volume == nil || *m.Volume != 550 {
		t.Errorf("volume: expected 550, got %v", m.Volume)
	}
	// wins: p1, p3 → 2/3
	if m.WinRate == nil || !near(*m.WinRate, 2.0/3.0*100) {
		t.Errorf("win_rate: expected 66.666..., got %v", m.WinRate)
	}
	// roi = 20/550*100
	if m.Roi == nil || !near(*m.Roi, 20.0/550.0*100) {
		t.Errorf("roi: expected %v, got %v", 20.0/550.0*100, m.Roi)
	}
	if m.TradeCount == nil || *m.TradeCount != 3 {
		t.Errorf("trade_count: expected 3, got %v", m.TradeCount)
	}
	if m.AvgPosition == nil || !near(*m.AvgPosition, 550.0/3.0) {
		t.Errorf("avg_position: expected %v, got %v", 550.0/3.0, m.AvgPosition)
	}
}

// SCAN-U-14: breakeven trade (PnL = 0) is not a win.
func TestComputeMetrics_Breakeven_NotAWin(t *testing.T) {
	fills := []MetricFill{
		{PositionID: "win", Side: "LONG", Timestamp: ts("2026-09-01T00:00:00Z"), Quantity: 1, Price: 10, RealizedPnl: 5},
		{PositionID: "flat", Side: "LONG", Timestamp: ts("2026-09-01T01:00:00Z"), Quantity: 1, Price: 10, RealizedPnl: 0},
		{PositionID: "loss", Side: "SHORT", Timestamp: ts("2026-09-01T02:00:00Z"), Quantity: 1, Price: 10, RealizedPnl: -5},
	}

	m := ComputeMetrics(fills, MetricWindow{})
	if m.WinRate == nil || !near(*m.WinRate, 1.0/3.0*100) {
		t.Errorf("breakeven must not count as win: expected %v, got %v", 1.0/3.0*100, m.WinRate)
	}
}

// SCAN-U-15: partial closes on one logical position count as 1 trade.
func TestComputeMetrics_PartialCloses_OneLogicalTrade(t *testing.T) {
	fills := []MetricFill{
		{PositionID: "pos-1", Side: "LONG", Timestamp: ts("2026-09-01T00:00:00Z"), Quantity: 1, Price: 100, RealizedPnl: 5},
		{PositionID: "pos-1", Side: "LONG", Timestamp: ts("2026-09-01T01:00:00Z"), Quantity: 1, Price: 100, RealizedPnl: 5},
		{PositionID: "pos-1", Side: "LONG", Timestamp: ts("2026-09-01T02:00:00Z"), Quantity: 1, Price: 100, RealizedPnl: 5},
	}

	m := ComputeMetrics(fills, MetricWindow{})
	if m.TradeCount == nil || *m.TradeCount != 1 {
		t.Errorf("trade_count: expected 1, got %v", m.TradeCount)
	}
	if m.RealizedPnl == nil || *m.RealizedPnl != 15 {
		t.Errorf("pnl: expected 15, got %v", m.RealizedPnl)
	}
	if m.LongCount == nil || *m.LongCount != 1 {
		t.Errorf("long_count: expected 1, got %v", m.LongCount)
	}
}

// SCAN-U-16: no data in the window → every metric nil, never 0 (BR-07).
func TestComputeMetrics_NoData_AllNil(t *testing.T) {
	m := ComputeMetrics(nil, MetricWindow{})
	if m == nil {
		t.Fatal("expected metrics object")
	}
	assertAllNil(t, m)
}

func assertAllNil(t *testing.T, m *Metrics) {
	t.Helper()
	if m.RealizedPnl != nil || m.Roi != nil || m.WinRate != nil || m.Volume != nil ||
		m.TradeCount != nil || m.AvgPosition != nil || m.AvgLeverage != nil ||
		m.LongCount != nil || m.ShortCount != nil || m.LastActiveAt != nil {
		t.Errorf("expected all metrics nil, got %+v", m)
	}
}

// SCAN-U-17: long vs short counts are per logical position.
func TestComputeMetrics_LongShortCounts(t *testing.T) {
	fills := []MetricFill{
		{PositionID: "l1", Side: "LONG", Timestamp: ts("2026-09-01T00:00:00Z"), Quantity: 1, Price: 10, RealizedPnl: 1},
		{PositionID: "l2", Side: "LONG", Timestamp: ts("2026-09-01T01:00:00Z"), Quantity: 1, Price: 10, RealizedPnl: 1},
		{PositionID: "s1", Side: "SHORT", Timestamp: ts("2026-09-01T02:00:00Z"), Quantity: 1, Price: 10, RealizedPnl: -1},
	}
	m := ComputeMetrics(fills, MetricWindow{})
	if m.LongCount == nil || *m.LongCount != 2 {
		t.Errorf("long_count: expected 2, got %v", m.LongCount)
	}
	if m.ShortCount == nil || *m.ShortCount != 1 {
		t.Errorf("short_count: expected 1, got %v", m.ShortCount)
	}
}

// SCAN-U-18: last_active = max fill timestamp, normalized to UTC (BR-08).
func TestComputeMetrics_LastActive_MaxTimestampUTC(t *testing.T) {
	loc := time.FixedZone("UTC+7", 7*3600)
	fills := []MetricFill{
		{PositionID: "a", Side: "LONG", Timestamp: ts("2026-09-01T00:00:00Z"), Quantity: 1, Price: 1},
		{PositionID: "b", Side: "LONG", Timestamp: time.Date(2026, 9, 2, 15, 0, 0, 0, loc), Quantity: 1, Price: 1},
	}
	m := ComputeMetrics(fills, MetricWindow{})
	if m.LastActiveAt == nil {
		t.Fatal("expected last_active_at")
	}
	if !m.LastActiveAt.Equal(ts("2026-09-02T08:00:00Z")) {
		t.Errorf("expected 2026-09-02T08:00:00Z, got %v", m.LastActiveAt)
	}
	if m.LastActiveAt.Location() != time.UTC {
		t.Errorf("expected UTC location, got %v", m.LastActiveAt.Location())
	}
}

// SCAN-U-19: metrics only include fills inside the requested window
// (24H vs 7D isolation, BE-04).
func TestComputeMetrics_TimeframeWindow_Isolated(t *testing.T) {
	now := ts("2026-09-10T00:00:00Z")
	fills := []MetricFill{
		{PositionID: "recent", Side: "LONG", Timestamp: now.Add(-1 * time.Hour), Quantity: 1, Price: 100, RealizedPnl: 10},
		{PositionID: "old", Side: "LONG", Timestamp: now.Add(-8 * 24 * time.Hour), Quantity: 1, Price: 100, RealizedPnl: 50},
	}

	day := ComputeMetrics(fills, MetricWindow{Start: now.Add(-24 * time.Hour), End: now.Add(time.Minute)})
	if day.TradeCount == nil || *day.TradeCount != 1 {
		t.Errorf("24H trade_count: expected 1, got %v", day.TradeCount)
	}
	if day.RealizedPnl == nil || *day.RealizedPnl != 10 {
		t.Errorf("24H pnl: expected 10, got %v", day.RealizedPnl)
	}

	week := ComputeMetrics(fills, MetricWindow{Start: now.Add(-7 * 24 * time.Hour), End: now.Add(time.Minute)})
	if week.TradeCount == nil || *week.TradeCount != 1 {
		t.Errorf("7D trade_count: expected 1 (8-day-old fill outside), got %v", week.TradeCount)
	}

	all := ComputeMetrics(fills, MetricWindow{})
	if all.TradeCount == nil || *all.TradeCount != 2 {
		t.Errorf("ALL trade_count: expected 2, got %v", all.TradeCount)
	}
}
