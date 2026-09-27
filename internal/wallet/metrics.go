package wallet

import (
	"time"
)

// MetricFill is one execution fill of an external wallet, attributed to a
// logical position (1 position = 1 trade, locked decision).
type MetricFill struct {
	PositionID  string
	Side        string // LONG | SHORT
	Timestamp   time.Time
	Quantity    float64
	Price       float64
	RealizedPnl float64
	Leverage    float64 // 0 = unknown
}

// MetricWindow bounds the computed timeframe (zero Start/End = unbounded).
// Timestamps are compared in UTC (BE-04).
type MetricWindow struct {
	Start time.Time
	End   time.Time
}

type logicalTrade struct {
	side string
	pnl  float64
}

// ComputeMetrics computes the canonical metric set for the fills inside the
// window (BE-04 / TEST-02):
//
//   - logical trades are grouped by PositionID (partial closes = 1 trade)
//   - win = realized PnL strictly > 0 (breakeven is not a win)
//   - win_rate is a percentage 0..100; roi = pnl/volume * 100
//   - no data in the window → every metric nil, never 0 (BR-07)
//   - last_active = max fill timestamp in UTC (BR-08)
func ComputeMetrics(fills []MetricFill, win MetricWindow) *Metrics {
	inWindow := make([]MetricFill, 0, len(fills))
	for _, f := range fills {
		ts := f.Timestamp.UTC()
		if !win.Start.IsZero() && ts.Before(win.Start.UTC()) {
			continue
		}
		if !win.End.IsZero() && !ts.Before(win.End.UTC()) {
			continue
		}
		inWindow = append(inWindow, f)
	}

	if len(inWindow) == 0 {
		return &Metrics{} // all nil (SCAN-U-16)
	}

	var (
		totalPnl     float64
		totalVolume  float64
		levSum       float64
		levN         int
		trades       = map[string]*logicalTrade{}
		order        []string
		lastActiveAt = inWindow[0].Timestamp.UTC()
	)

	for _, f := range inWindow {
		ts := f.Timestamp.UTC()
		if ts.After(lastActiveAt) {
			lastActiveAt = ts
		}
		totalPnl += f.RealizedPnl
		totalVolume += abs(f.Quantity * f.Price)
		if f.Leverage > 0 {
			levSum += f.Leverage
			levN++
		}

		id := f.PositionID
		if id == "" {
			id = ts.Format(time.RFC3339Nano) // ungrouped fill = own trade
		}
		t, ok := trades[id]
		if !ok {
			t = &logicalTrade{side: f.Side}
			trades[id] = t
			order = append(order, id)
		}
		t.pnl += f.RealizedPnl
	}

	var wins, longs, shorts int
	for _, t := range trades {
		if t.pnl > 0 { // SCAN-U-14: breakeven (0) is not a win
			wins++
		}
		switch t.side {
		case "SHORT":
			shorts++
		default:
			longs++
		}
	}

	tradeCount := int64(len(trades))
	winRate := float64(wins) / float64(tradeCount) * 100

	m := &Metrics{
		RealizedPnl:  f64(totalPnl),
		Volume:       f64(totalVolume),
		TradeCount:   &tradeCount,
		LongCount:    i64(int64(longs)),
		ShortCount:   i64(int64(shorts)),
		LastActiveAt: &lastActiveAt,
	}
	if totalVolume > 0 {
		m.Roi = f64(totalPnl / totalVolume * 100)
		m.AvgPosition = f64(totalVolume / float64(tradeCount))
	}
	m.WinRate = f64(winRate)
	if levN > 0 {
		m.AvgLeverage = f64(levSum / float64(levN))
	}
	return m
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func f64(v float64) *float64 { return &v }
func i64(v int64) *int64     { return &v }
