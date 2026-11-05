package trader

import (
	"context"
	"errors"
	"testing"

	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
	"github.com/qwerty7415963/go_be_arbitrage/internal/hyperliquid"
)

func portfolioSeriesPoint(ms int64, v string) hyperliquid.PortfolioPoint {
	return hyperliquid.PortfolioPoint{Time: ms, Value: v}
}

func TestMapPortfolioSeries_ModeWindowMapping(t *testing.T) {
	raw := map[string]hyperliquid.PortfolioSeries{
		"day":      {AccountValue: []hyperliquid.PortfolioPoint{portfolioSeriesPoint(1, "10")}, PnL: []hyperliquid.PortfolioPoint{portfolioSeriesPoint(1, "1")}},
		"week":     {AccountValue: []hyperliquid.PortfolioPoint{portfolioSeriesPoint(2, "20")}},
		"month":    {AccountValue: []hyperliquid.PortfolioPoint{portfolioSeriesPoint(3, "30")}},
		"allTime":  {AccountValue: []hyperliquid.PortfolioPoint{portfolioSeriesPoint(4, "40")}},
		"perpDay":  {AccountValue: []hyperliquid.PortfolioPoint{portfolioSeriesPoint(5, "50")}, PnL: []hyperliquid.PortfolioPoint{portfolioSeriesPoint(5, "5")}},
		"perpWeek": {AccountValue: []hyperliquid.PortfolioPoint{portfolioSeriesPoint(6, "60")}},
		"perpMonth": {
			AccountValue: []hyperliquid.PortfolioPoint{portfolioSeriesPoint(7, "70")},
		},
		"perpAllTime": {AccountValue: []hyperliquid.PortfolioPoint{portfolioSeriesPoint(8, "80")}},
	}
	perps := mapPortfolioSeries(raw, PerformanceModePerps)
	combined := mapPortfolioSeries(raw, PerformanceModeCombined)
	for _, modeSeries := range []map[string]RangeSeriesDTO{perps, combined} {
		if len(modeSeries) != len(performanceRangeKeys) {
			t.Fatalf("want 4 ranges, got %v", modeSeries)
		}
		for _, rng := range performanceRangeKeys {
			rs, ok := modeSeries[rng]
			if !ok || rs.AccountValue == nil || rs.PnL == nil {
				t.Fatalf("range %s missing/null: %+v", rng, rs)
			}
		}
	}
	if got := perps["24H"].AccountValue[0].Value; got != 50 {
		t.Errorf("perps 24H must use perpDay, got %v", got)
	}
	if got := perps["ALL"].AccountValue[0].Value; got != 80 {
		t.Errorf("perps ALL must use perpAllTime, got %v", got)
	}
	if got := combined["24H"].AccountValue[0].Value; got != 10 {
		t.Errorf("combined 24H must use day, got %v", got)
	}
	if got := combined["30D"].AccountValue[0].Value; got != 30 {
		t.Errorf("combined 30D must use month, got %v", got)
	}
}

func TestMapPortfolioSeries_SortsAndSkipsInvalid(t *testing.T) {
	raw := map[string]hyperliquid.PortfolioSeries{
		"perpDay": {
			AccountValue: []hyperliquid.PortfolioPoint{
				portfolioSeriesPoint(3000, "3"),
				portfolioSeriesPoint(1000, "1"),
				portfolioSeriesPoint(2000, "oops"),
				portfolioSeriesPoint(0, "9"),
				portfolioSeriesPoint(4000, "4"),
			},
			PnL: []hyperliquid.PortfolioPoint{portfolioSeriesPoint(200, "-2"), portfolioSeriesPoint(100, "-1")},
		},
	}
	got := mapPortfolioSeries(raw, PerformanceModePerps)["24H"]
	if len(got.AccountValue) != 3 {
		t.Fatalf("invalid/zero points must be skipped: %+v", got.AccountValue)
	}
	for i := 1; i < len(got.AccountValue); i++ {
		if got.AccountValue[i].Time.Before(got.AccountValue[i-1].Time) {
			t.Fatalf("points must sort ASC: %+v", got.AccountValue)
		}
	}
	if len(got.PnL) != 2 || !got.PnL[0].Time.Before(got.PnL[1].Time) {
		t.Fatalf("pnl points must sort ASC: %+v", got.PnL)
	}
}

func TestPerformance_InvalidMode(t *testing.T) {
	s := NewService(nil, nil, []byte("series-test-secret-32-bytes!!!"))
	_, err := s.Performance(context.Background(), "hyperliquid",
		"0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Period30D, "bogus", false)
	if err == nil {
		t.Fatal("invalid mode must error")
	}
	var appErr *domain.AppError
	if !errors.As(err, &appErr) || appErr.Code != domain.ErrCodeInvalidFilter {
		t.Fatalf("want INVALID_FILTER, got %v", err)
	}
}

func TestPerformanceCacheKey_ModeSeparates(t *testing.T) {
	another := performanceCacheKey("hyperliquid", "0xabc", Period30D, PerformanceModeCombined)
	if another == performanceCacheKey("hyperliquid", "0xabc", Period30D, PerformanceModePerps) {
		t.Fatal("perps/combined must use separate cache keys")
	}
	empty, ok := normalizePerformanceMode("")
	if !ok || empty != PerformanceModePerps {
		t.Fatalf("empty mode must normalize to perps: %q %v", empty, ok)
	}
}

func TestEnsureSeries_FillsAllRangesNonNil(t *testing.T) {
	got := ensureSeries(map[string]RangeSeriesDTO{"24H": {}})
	if len(got) != len(performanceRangeKeys) {
		t.Fatalf("want all ranges, got %v", got)
	}
	for _, rng := range performanceRangeKeys {
		rs := got[rng]
		if rs.AccountValue == nil || rs.PnL == nil {
			t.Errorf("range %s must have non-nil arrays: %+v", rng, rs)
		}
	}
}
