package trader

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/hyperliquid"
)

var errParseFloat = errors.New("invalid float")

// LiveLBRefFresh bounds leaderboard ROI/PnL passthrough freshness for ALL
// (mirrors sync LBRefFresh 24h; live-read per §1.7).
const LiveLBRefFresh = 24 * time.Hour

// LivePerformanceSnapshot is the cached live performance for one
// (wallet,venue,period): metrics + equity + fetch metadata. Cached 12s +
// single-flight.
type LivePerformanceSnapshot struct {
	Metrics *PerformanceMetricsDTO
	Equity  []EquityPointDTO
	AsOf    time.Time
	Partial bool
}

// periodFillsLookback maps 1D/7D/30D to fills windows. ALL has no fills window
// (portfolio/LB only).
func periodFillsLookback(period string) (time.Duration, bool) {
	switch period {
	case Period1D:
		return 24 * time.Hour, true
	case Period7D:
		return 7 * 24 * time.Hour, true
	case Period30D:
		return 30 * 24 * time.Hour, true
	}
	return 0, false
}

// computeLiveMetrics folds reconstructed trades into performance metrics.
// pnl = realized net sum (Σ(PnL−Fees)); roi = pnl/volume fallback;
// win_rate excludes breakeven; profit_factor never Infinity/NaN;
// maxDD from live equity when available else nil. asOf = fetch time.
// status is ready on success; callers override to error on degradation.
func computeLiveMetrics(trades []CompletedTrade, equityCurve []float64, asOf time.Time, partial bool, status DataStatus) *PerformanceMetricsDTO {
	var pnl, volume, gp, gl float64
	var wins, losses int64
	var lw, sw, lc, sc int64
	for _, t := range trades {
		net := t.Net()
		pnl += net
		volume += t.Volume
		if net > 0 {
			gp += net
		} else if net < 0 {
			gl += net
		}
		switch {
		case t.Breakeven():
			// excluded from win rate
		case t.Win():
			wins++
		default:
			losses++
		}
		if t.Long {
			lc++
			if t.Win() {
				lw++
			}
		} else {
			sc++
			if t.Win() {
				sw++
			}
		}
	}
	tc := int64(len(trades))
	m := &PerformanceMetricsDTO{
		PnL: &pnl, Volume: &volume, TradeCount: &tc,
		WinRate:      WinRatePct(wins, losses),
		ProfitFactor: ProfitFactor(&gp, &gl),
		LongWins:     &lw, LongCount: &lc, ShortWins: &sw, ShortCount: &sc,
		DataStatus: status, IsPartial: partial,
	}
	m.ROI = FallbackROIPct(&pnl, &volume)
	if len(equityCurve) > 0 {
		m.MaxDrawdownPct = MaxDrawdownPct(equityCurve)
	}
	asOfUTC := asOf.UTC()
	m.MetricsAsOf = &asOfUTC
	return m
}

// aggregateLiveEquity folds portfolio histories into dated daily points for
// [from, now] oldest-first (reuse AggregateEquityDaily). Returns DTOs +
// end-equity curve for drawdown.
func aggregateLiveEquity(raw map[string][]hyperliquid.PortfolioPoint, from, now time.Time) ([]EquityPointDTO, []float64) {
	var points []EquityPoint
	for _, w := range []string{"day", "week", "month", "allTime"} {
		for _, p := range raw[w] {
			v, err := parsePortfolioValue(p.Value)
			if err != nil || p.Time <= 0 {
				continue
			}
			t := time.UnixMilli(p.Time).UTC()
			if t.Before(from) || t.After(now.Add(time.Second)) {
				continue
			}
			points = append(points, EquityPoint{Time: t, Value: v})
		}
	}
	byDay := AggregateEquityDaily(points)
	keys := make([]string, 0, len(byDay))
	for k := range byDay {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	equity := make([]EquityPointDTO, 0, len(keys))
	curve := make([]float64, 0, len(keys))
	for _, k := range keys {
		d := byDay[k]
		equity = append(equity, EquityPointDTO{
			Date:      d.Date.UTC().Format("2006-01-02"),
			EndEquity: d.EndEquity, DailyReturn: d.DailyReturn,
		})
		if d.EndEquity != nil {
			curve = append(curve, *d.EndEquity)
		}
	}
	if equity == nil {
		equity = []EquityPointDTO{}
	}
	return equity, curve
}

func parsePortfolioValue(s string) (float64, error) {
	return parseFloatStrict(s)
}

func parseFloatStrict(s string) (float64, error) {
	t := strings.TrimSpace(s)
	if t == "" {
		return 0, errParseFloat
	}
	f, err := strconv.ParseFloat(t, 64)
	if err != nil {
		return 0, err
	}
	return f, nil
}

// fetchLivePerformance computes one period's live performance:
// 1D/7D/30D from the shared 30d fills universe (slice + reconstruct reuse) +
// live equity; ALL from portfolio allTime + LB refs passthrough (live-read).
// The fills leg never fetches directly: 1D/7D/30D slice the 30d universe
// (getFillsUniverse, single fetch per wallet per TTL); portfolio/LB stay
// separate direct fetches. as_of is the universe fetch time. Truncated
// universes slice + emit partial:true without any refill fetch.
// No reads of trader_period_metrics / trader_equity_daily (conformance §7);
// LB refs are read live (existing resolve semantics).
func fetchLivePerformance(ctx context.Context, s *Service, venueID uuid.UUID, addr, venue, period string, now time.Time) (*LivePerformanceSnapshot, error) {
	if s.onDemand == nil {
		return nil, errUpstreamUnavailable
	}
	from := now.Add(-periodLookback(period))
	// Equity is always live (portfolio + AggregateEquityDaily reuse).
	rawPort, portErr := s.onDemand.FetchPortfolio(ctx, addr)
	var equity []EquityPointDTO
	var curve []float64
	if portErr == nil {
		equity, curve = aggregateLiveEquity(rawPort, from, now)
	} else {
		equity = []EquityPointDTO{}
	}

	if period == PeriodALL {
		// ALL: LB passthrough (fresh) + live equity. No fills fetch.
		window := LBWindowForPeriod[PeriodALL]
		var roi, pnl, vol *float64
		if ref, err := s.repo.GetLeaderboardRef(ctx, venueID, addr, window); err == nil && ref != nil &&
			now.Sub(ref.FetchedAt) <= LiveLBRefFresh {
			if ref.ROI != nil {
				v := *ref.ROI * 100
				roi = &v
			}
			pnl = ref.PnL
			vol = ref.Volume
		}
		var m *PerformanceMetricsDTO
		if roi != nil || pnl != nil || vol != nil {
			asOf := now.UTC()
			m = &PerformanceMetricsDTO{
				ROI: roi, PnL: pnl, Volume: vol,
				DataStatus: DataReady, IsPartial: false, MetricsAsOf: &asOf,
			}
			if len(curve) > 0 {
				m.MaxDrawdownPct = MaxDrawdownPct(curve)
			}
		} else if portErr != nil {
			return nil, portErr
		}
		// LB missing + portfolio ok ⇒ equity-only snapshot (metrics null,
		// still ready: ALL has no fills universe to be partial about).
		if m == nil && portErr == nil {
			return &LivePerformanceSnapshot{Metrics: nil, Equity: equity, AsOf: now.UTC(), Partial: false}, nil
		}
		if m == nil {
			return nil, errUpstreamUnavailable
		}
		return &LivePerformanceSnapshot{Metrics: m, Equity: equity, AsOf: now.UTC(), Partial: false}, nil
	}

	// 1D/7D/30D: shared 30d universe → slice → reconstruct → metrics + live equity drawdown.
	lookback, ok := periodFillsLookback(period)
	if !ok {
		lookback = 30 * 24 * time.Hour
	}
	universe, err := getFillsUniverse(ctx, s, venue, addr, now)
	if err != nil {
		return nil, err
	}
	sliced := sliceUniverse(universe.Fills, universe.FetchedAt, lookback)
	trades := ReconstructTrades(sliced)
	metrics := computeLiveMetrics(trades, curve, universe.FetchedAt, universe.Truncated, DataReady)
	// If equity fetch failed but fills succeeded, still ready (drawdown null).
	return &LivePerformanceSnapshot{Metrics: metrics, Equity: equity, AsOf: universe.FetchedAt.UTC(), Partial: universe.Truncated}, nil
}
