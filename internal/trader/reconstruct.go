package trader

import (
	"sort"
	"time"
)

// CompletedTrade is one closed position cycle (spec §10): fills aggregated
// chronologically per wallet+coin+direction; partial entries and partial
// exits share the cycle; reaching flat emits the trade; a direct long→short
// flip closes the long first and opens a new short cycle. Close time is the
// period attribution time. Net = Σ closedPnl − Σ fees; funding is tracked
// separately and never decides win/loss.
type CompletedTrade struct {
	Market    string
	Long      bool // cycle direction
	OpenTime  time.Time
	CloseTime time.Time
	Volume    float64 // Σ |qty*price| over cycle fills
	PnL       float64 // Σ closedPnl over cycle fills
	Fees      float64 // Σ fees over cycle fills
	Fills     int
}

// Net is the cycle's net PnL.
func (t CompletedTrade) Net() float64 { return t.PnL - t.Fees }

// Win reports net > 0; Breakeven reports net == 0 (excluded from win rate).
func (t CompletedTrade) Win() bool       { return t.Net() > 0 }
func (t CompletedTrade) Breakeven() bool { return t.Net() == 0 }

// HoldingSeconds is the cycle length (completed trades only).
func (t CompletedTrade) HoldingSeconds() float64 {
	return t.CloseTime.Sub(t.OpenTime).Seconds()
}

func sgn(f float64) int {
	switch {
	case f > 0:
		return 1
	case f < 0:
		return -1
	}
	return 0
}

// ReconstructTrades folds fills into completed cycles. Fills need not be
// sorted. Zero-quantity fills are ignored. A history that starts mid-position
// opens its first cycle at the first fill (documented truncation limitation).
func ReconstructTrades(fills []Fill) []CompletedTrade {
	ordered := append([]Fill(nil), fills...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].FilledAt.Equal(ordered[j].FilledAt) {
			return ordered[i].Tid < ordered[j].Tid
		}
		return ordered[i].FilledAt.Before(ordered[j].FilledAt)
	})

	type cycle struct {
		market string
		long   bool
		open   time.Time
		volume float64
		pnl    float64
		fees   float64
		fills  int
	}
	open := map[string]*cycle{} // market -> open cycle
	pos := map[string]float64{} // market -> signed open size
	var out []CompletedTrade

	emit := func(m string, c *cycle, at time.Time) {
		out = append(out, CompletedTrade{
			Market: m, Long: c.long, OpenTime: c.open, CloseTime: at,
			Volume: c.volume, PnL: c.pnl, Fees: c.fees, Fills: c.fills,
		})
	}

	for _, f := range ordered {
		if f.Quantity == 0 {
			continue
		}
		q := f.Quantity
		if !f.Buy {
			q = -q
		}
		before := pos[f.Market]
		after := before + q

		c := open[f.Market]
		if c == nil {
			c = &cycle{market: f.Market, long: q > 0, open: f.FilledAt}
			open[f.Market] = c
		}
		c.volume += abs(f.Quantity * f.Price)
		c.pnl += f.ClosedPnL
		c.fees += f.Fee
		c.fills++

		if before != 0 && sgn(after) != sgn(before) {
			// Flat or flipped: close the old cycle at this fill (BE-013/014).
			emit(f.Market, c, f.FilledAt)
			delete(open, f.Market)
			if after != 0 {
				// Flip: the same fill opens the new cycle (time only —
				// size/pnl/fees stay attributed to the closed one).
				open[f.Market] = &cycle{market: f.Market, long: after > 0, open: f.FilledAt}
			}
		}
		pos[f.Market] = after
	}
	return out
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

// RollupDay aggregates one UTC day from its fills + the trades closed that day
// (close-time attribution, spec §10.6). Counts/PnL come from completed trades;
// volume/fees from the day's fills. Recompute replaces — reruns are identical
// (BE-020).
func RollupDay(venueIDOfDay, addr string, day time.Time, fills []Fill, trades []CompletedTrade) *DailyStats {
	_ = venueIDOfDay
	_ = addr
	s := &DailyStats{StatDate: day.UTC().Truncate(24 * time.Hour)}
	var pnl, fees, volume, grossProfit, grossLoss, holdSum float64
	var holdN int64
	var lastTrade *time.Time
	for _, t := range trades {
		net := t.Net()
		s.TradeCount++
		switch {
		case t.Breakeven():
			s.BreakevenCount++
		case t.Win():
			s.WinCount++
		default:
			s.LossCount++
		}
		pnl += net
		if net > 0 {
			grossProfit += net
		} else if net < 0 {
			grossLoss += net
		}
		if t.Long {
			s.LongCount++
			if t.Win() {
				s.LongWins++
			}
		} else {
			s.ShortCount++
			if t.Win() {
				s.ShortWins++
			}
		}
		holdSum += t.HoldingSeconds()
		holdN++
		ct := t.CloseTime
		if lastTrade == nil || ct.After(*lastTrade) {
			lastTrade = &ct
		}
	}
	for _, f := range fills {
		volume += abs(f.Quantity * f.Price)
		fees += f.Fee
	}
	s.RealizedPnL = &pnl
	s.Fees = fees
	s.Volume = &volume
	s.GrossProfit = &grossProfit
	s.GrossLoss = &grossLoss
	s.HoldingTimeSecSum = holdSum
	s.HoldingTimeSecCount = holdN
	s.LastTradeAt = lastTrade
	return s
}

// WinRatePct returns 100*wins/(wins+losses), nil when no decided trades
// (breakeven excluded, spec §9/BE-016).
func WinRatePct(wins, losses int64) *float64 {
	if wins+losses == 0 {
		return nil
	}
	v := 100 * float64(wins) / float64(wins+losses)
	return &v
}

// ProfitFactor returns grossProfit/|grossLoss|, nil when grossLoss is
// missing or zero (never Infinity/NaN, spec BE-017/BE-018).
func ProfitFactor(grossProfit, grossLoss *float64) *float64 {
	if grossProfit == nil || grossLoss == nil || *grossLoss == 0 {
		return nil
	}
	gl := *grossLoss
	if gl < 0 {
		gl = -gl
	}
	v := *grossProfit / gl
	return &v
}

// AvgTradePnL returns pnl/trades, nil when no trades.
func AvgTradePnL(pnl *float64, trades int64) *float64 {
	if pnl == nil || trades == 0 {
		return nil
	}
	v := *pnl / float64(trades)
	return &v
}

// AvgHoldingSec returns sum/count, nil when no completed trades.
func AvgHoldingSec(sum float64, n int64) *float64 {
	if n == 0 {
		return nil
	}
	v := sum / float64(n)
	return &v
}

// FallbackROIPct is the documented v1 estimate used only when no fresh
// leaderboard window maps to the period: 100*pnl/volume, nil when volume is
// missing or zero (spec v1.1 D4; calculation_version stamps the choice).
func FallbackROIPct(pnl, volume *float64) *float64 {
	if pnl == nil || volume == nil || *volume == 0 {
		return nil
	}
	v := 100 * *pnl / *volume
	return &v
}
