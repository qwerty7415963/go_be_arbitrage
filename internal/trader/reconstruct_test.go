package trader

import (
	"testing"
	"time"
)

func mkFill(market string, tid int64, ts time.Time, buy bool, qty, price, closed, fee float64) Fill {
	return Fill{Market: market, Tid: tid, FilledAt: ts, Buy: buy,
		Quantity: qty, Price: price, ClosedPnL: closed, Fee: fee}
}

func t0() time.Time { return time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC) }

// TRD: fixture A — one simple long winner.
func TestReconstruct_LongWin(t *testing.T) {
	trades := ReconstructTrades([]Fill{
		mkFill("BTC", 1, t0(), true, 1, 100, 0, 0.05),
		mkFill("BTC", 2, t0().Add(time.Hour), false, 1, 110, 10, 0.05),
	})
	if len(trades) != 1 {
		t.Fatalf("want 1 trade, got %d", len(trades))
	}
	tr := trades[0]
	if !tr.Long || !tr.Win() || tr.Breakeven() {
		t.Errorf("long win: %+v", tr)
	}
	if tr.Net() != 9.9 {
		t.Errorf("net = closedPnl - fees: %v", tr.Net())
	}
	if tr.HoldingSeconds() != 3600 || tr.Fills != 2 {
		t.Errorf("holding/fills: %+v", tr)
	}
}

// TRD: fixture B — one short loser.
func TestReconstruct_ShortLoss(t *testing.T) {
	trades := ReconstructTrades([]Fill{
		mkFill("ETH", 1, t0(), false, 1, 100, 0, 0.05),
		mkFill("ETH", 2, t0().Add(30*time.Minute), true, 1, 105, -5, 0.05),
	})
	if len(trades) != 1 || trades[0].Long || trades[0].Win() {
		t.Fatalf("short loss: %+v", trades)
	}
	if trades[0].Net() != -5.1 {
		t.Errorf("net: %v", trades[0].Net())
	}
}

// TRD-U-01 (BE-012/013): fixture C — partial entries + partial exits = 1 trade.
func TestReconstruct_PartialEntryExit(t *testing.T) {
	trades := ReconstructTrades([]Fill{
		mkFill("BTC", 1, t0(), true, 1, 100, 0, 0),
		mkFill("BTC", 2, t0().Add(time.Minute), true, 1, 102, 0, 0),
		mkFill("BTC", 3, t0().Add(2*time.Minute), false, 1, 105, 3, 0),
		mkFill("BTC", 4, t0().Add(3*time.Minute), false, 1, 106, 4, 0),
	})
	if len(trades) != 1 {
		t.Fatalf("want 1 trade, got %d", len(trades))
	}
	if trades[0].PnL != 7 || trades[0].Fills != 4 {
		t.Errorf("aggregate: %+v", trades[0])
	}
}

// TRD-U-02 (BE-014): fixture D — long flips directly to short.
func TestReconstruct_LongFlipShort(t *testing.T) {
	trades := ReconstructTrades([]Fill{
		mkFill("BTC", 1, t0(), true, 1, 100, 0, 0),
		mkFill("BTC", 2, t0().Add(time.Minute), false, 2, 110, 10, 0),
		mkFill("BTC", 3, t0().Add(2*time.Minute), true, 1, 108, 2, 0),
	})
	if len(trades) != 2 {
		t.Fatalf("want 2 trades, got %d", len(trades))
	}
	if !trades[0].Long || trades[1].Long {
		t.Errorf("directions must not mix: %+v", trades)
	}
	if trades[0].PnL != 10 || trades[1].PnL != 2 {
		t.Errorf("pnl attribution: %+v", trades)
	}
	if !trades[1].OpenTime.Equal(t0().Add(time.Minute)) {
		t.Errorf("flip opens new cycle at flip time: %v", trades[1].OpenTime)
	}
}

// TRD-U-03 (BE-015): fixture E — breakeven excluded from wins.
func TestReconstruct_Breakeven(t *testing.T) {
	trades := ReconstructTrades([]Fill{
		mkFill("BTC", 1, t0(), true, 1, 100, 0, 0),
		mkFill("BTC", 2, t0().Add(time.Minute), false, 1, 100, 0, 0),
	})
	if len(trades) != 1 || !trades[0].Breakeven() || trades[0].Win() {
		t.Fatalf("breakeven: %+v", trades)
	}
}

// TRD-U-04: fixture F — interleaved coins stay separate; unsorted input OK.
func TestReconstruct_MultiCoin(t *testing.T) {
	trades := ReconstructTrades([]Fill{
		mkFill("ETH", 2, t0().Add(time.Minute), true, 1, 52, -2, 0),
		mkFill("BTC", 1, t0(), true, 1, 100, 0, 0),
		mkFill("BTC", 3, t0().Add(2*time.Minute), false, 1, 110, 10, 0),
		mkFill("ETH", 1, t0(), false, 1, 50, 0, 0),
	})
	if len(trades) != 2 {
		t.Fatalf("want 2 trades, got %d", len(trades))
	}
	byMarket := map[string]CompletedTrade{}
	for _, tr := range trades {
		byMarket[tr.Market] = tr
	}
	if !byMarket["BTC"].Long || byMarket["ETH"].Long {
		t.Errorf("per-coin cycles: %+v", trades)
	}
}

// TRD-U-06 (fixture I): fees reduce net; funding has no field (tracked
// separately by construction — Fill carries no funding).
func TestReconstruct_Fees(t *testing.T) {
	trades := ReconstructTrades([]Fill{
		mkFill("BTC", 1, t0(), true, 2, 100, 0, 0.2),
		mkFill("BTC", 2, t0().Add(time.Minute), false, 2, 110, 20, 0.2),
	})
	if len(trades) != 1 || trades[0].Net() != 19.6 || trades[0].Fees != 0.4 {
		t.Fatalf("fee handling: %+v", trades)
	}
}

// Determinism (fixture G spirit): same input twice → identical output.
func TestReconstruct_Deterministic(t *testing.T) {
	in := []Fill{
		mkFill("BTC", 1, t0(), true, 1, 100, 0, 0),
		mkFill("BTC", 2, t0().Add(time.Minute), false, 1, 110, 10, 0),
	}
	a, b := ReconstructTrades(in), ReconstructTrades(in)
	if len(a) != 1 || len(b) != 1 || a[0] != b[0] {
		t.Errorf("not deterministic: %+v vs %+v", a, b)
	}
}

// Rollup: 1 long win + 1 short loss closed the same UTC day.
func TestRollupDay(t *testing.T) {
	day := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	fills := []Fill{
		mkFill("BTC", 1, day.Add(2*time.Hour), true, 1, 100, 0, 0.1),
		mkFill("BTC", 2, day.Add(3*time.Hour), false, 1, 110, 10, 0.1),
		mkFill("ETH", 3, day.Add(4*time.Hour), false, 1, 50, 0, 0.1),
		mkFill("ETH", 4, day.Add(5*time.Hour), true, 1, 55, -5, 0.1),
	}
	trades := ReconstructTrades(fills)
	s := RollupDay("", "", day, fills, trades)
	if s.TradeCount != 2 || s.WinCount != 1 || s.LossCount != 1 || s.BreakevenCount != 0 {
		t.Errorf("counts: %+v", s)
	}
	if *s.RealizedPnL < 4.59 || *s.RealizedPnL > 4.61 {
		t.Errorf("pnl = 9.8 + (-5.2): %v", *s.RealizedPnL)
	}
	if *s.GrossProfit < 9.79 || *s.GrossProfit > 9.81 ||
		*s.GrossLoss < -5.21 || *s.GrossLoss > -5.19 {
		t.Errorf("gross: %v %v", *s.GrossProfit, *s.GrossLoss)
	}
	if s.LongCount != 1 || s.LongWins != 1 || s.ShortCount != 1 || s.ShortWins != 0 {
		t.Errorf("long/short: %+v", s)
	}
	if *s.Volume != 100+110+50+55 {
		t.Errorf("volume: %v", *s.Volume)
	}
	if s.HoldingTimeSecCount != 2 || s.LastTradeAt == nil ||
		!s.LastTradeAt.Equal(day.Add(5*time.Hour)) {
		t.Errorf("holding/last: %+v", s)
	}
	if !s.StatDate.Equal(day) {
		t.Errorf("UTC date: %v", s.StatDate)
	}
}

// MET-U-01/02 + formula helpers.
func TestMetricFormulas(t *testing.T) {
	if got := WinRatePct(2, 1); got == nil || *got < 66.66 || *got > 66.67 {
		t.Errorf("win rate 2/1/1BE: %v", got)
	}
	if WinRatePct(0, 0) != nil {
		t.Error("no decided trades must be nil")
	}
	gp, gl := 300.0, -100.0
	if got := ProfitFactor(&gp, &gl); got == nil || *got != 3.0 {
		t.Errorf("PF: %v", got)
	}
	zero := 0.0
	if ProfitFactor(&gp, &zero) != nil || ProfitFactor(&gp, nil) != nil {
		t.Error("zero/missing loss must be null, never Inf")
	}
	pnl := 90.0
	if got := AvgTradePnL(&pnl, 3); got == nil || *got != 30.0 {
		t.Errorf("avg: %v", got)
	}
	if AvgTradePnL(&pnl, 0) != nil || AvgTradePnL(nil, 3) != nil {
		t.Error("empty avg must be nil")
	}
	if got := AvgHoldingSec(90, 3); got == nil || *got != 30 {
		t.Errorf("holding: %v", got)
	}
	vol := 1000.0
	if got := FallbackROIPct(&pnl, &vol); got == nil || *got != 9.0 {
		t.Errorf("fallback ROI: %v", got)
	}
	if FallbackROIPct(&pnl, &zero) != nil || FallbackROIPct(nil, &vol) != nil {
		t.Error("fallback ROI must be nil without volume")
	}
}

func TestBackoffDelay(t *testing.T) {
	if backoffDelay(0) != 5*time.Minute || backoffDelay(1) != 10*time.Minute {
		t.Errorf("backoff: %v %v", backoffDelay(0), backoffDelay(1))
	}
	if backoffDelay(99) != 6*time.Hour {
		t.Errorf("cap: %v", backoffDelay(99))
	}
}
