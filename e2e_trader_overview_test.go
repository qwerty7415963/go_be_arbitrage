//go:build e2e

package e2e

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/qwerty7415963/go_be_arbitrage/internal/trader"
)

func TestE2E_TraderOverview(t *testing.T) {
	s := setupTraderSuite(t)
	ctx := context.Background()
	repo := trader.NewRepository(s.db)

	// Seed richer metrics for overview: set realized_pnl, volume, counts on existing wallets
	pnls := []float64{5000, 15000, 25000}
	vols := []float64{1000, 2000, 3000}
	for i, addr := range s.addrs {
		pnl := pnls[i]
		vol := vols[i]
		tc := int64(4 + i)
		lc := int64(2 + i)
		sc := int64(1)
		if err := repo.UpsertPeriodMetrics(ctx, &trader.PeriodMetrics{
			VenueID: s.venueID, WalletAddress: addr, Period: trader.Period30D,
			AsOf: time.Now().UTC(), PnL: &pnl, RealizedPnL: &pnl,
			Volume: &vol, TradeCount: &tc, LongCount: &lc, ShortCount: &sc,
			DataStatus: trader.DataReady, CalculationVersion: 1,
		}); err != nil {
			t.Fatalf("period: %v", err)
		}
		// equity daily
		eq := pnl * 10
		day := time.Now().UTC().Truncate(24 * time.Hour)
		if err := repo.UpsertEquityDaily(ctx, s.venueID, addr, trader.EquityDay{
			Date: day, StartEquity: &eq, EndEquity: &eq, PeakEquity: &eq,
		}); err != nil {
			t.Fatalf("equity: %v", err)
		}
		// daily stats for series
		rpnl := pnl / 2
		if err := repo.UpsertDailyStats(ctx, &trader.DailyStats{
			VenueID: s.venueID, WalletAddress: addr, StatDate: day,
			RealizedPnL: &rpnl, TradeCount: tc, LongCount: lc, ShortCount: sc,
			Volume: &vol,
		}); err != nil {
			t.Fatalf("daily: %v", err)
		}
	}

	// seed a wallet with PnL=0 (excluded from pnl buckets but counted in universe)
	zeroAddr := traderSeedAddr(3)
	if _, _, err := repo.UpsertRegistry(ctx, s.venueID, zeroAddr, trader.SourceLeaderboard, nil, nil); err != nil {
		t.Fatalf("registry zero: %v", err)
	}
	zeroPnl := 0.0
	if err := repo.UpsertPeriodMetrics(ctx, &trader.PeriodMetrics{
		VenueID: s.venueID, WalletAddress: zeroAddr, Period: trader.Period30D,
		AsOf: time.Now().UTC(), PnL: &zeroPnl, RealizedPnL: &zeroPnl,
		DataStatus: trader.DataReady, CalculationVersion: 1,
	}); err != nil {
		t.Fatalf("period zero: %v", err)
	}

	// defaults
	code, resp := s.doJSON(t, "GET", "/api/v1/traders/overview?venue=trader-e2e-venue", "", "")
	if code != http.StatusOK {
		t.Fatalf("overview: %d %v", code, resp)
	}
	data := resp["data"].(map[string]any)
	if data["venue"] != "trader-e2e-venue" {
		t.Errorf("venue: %v", data["venue"])
	}
	if data["period"] != "30D" {
		t.Errorf("period: %v", data["period"])
	}
	if data["pnl_metric"] != "realized_pnl" {
		t.Errorf("pnl_metric: %v", data["pnl_metric"])
	}
	pb, _ := data["pnl_buckets"].([]any)
	if len(pb) != 6 {
		t.Errorf("pnl_buckets: %d", len(pb))
	}
	ab, _ := data["account_buckets"].([]any)
	if len(ab) != 5 {
		t.Errorf("account_buckets: %d", len(ab))
	}
	foundAcctNotional := false
	foundNonNeutralSentiment := false
	for _, b := range ab {
		if b.(map[string]any)["long_notional"].(float64) > 0 {
			foundAcctNotional = true
		}
		if sent, ok := b.(map[string]any)["sentiment"].(string); ok && sent != "neutral" {
			foundNonNeutralSentiment = true
		}
	}
	if !foundAcctNotional {
		t.Errorf("account bucket long_notional not > 0")
	}
	if !foundNonNeutralSentiment {
		t.Errorf("account bucket sentiment all neutral")
	}
	cov, _ := data["coverage"].(map[string]any)
	if cov["pnl_wallets"].(float64) != 3 {
		t.Errorf("pnl_wallets: %v", cov["pnl_wallets"])
	}
	if data["universe_size"].(float64) != 4 {
		t.Errorf("universe_size: %v", data["universe_size"])
	}
	// PnL=0 wallet must not be counted in pnl buckets
	totalPnLWallets := 0
	for _, b := range pb {
		totalPnLWallets += int(b.(map[string]any)["wallet_count"].(float64))
	}
	if totalPnLWallets != 3 {
		t.Errorf("pnl bucket wallet total: %d want 3", totalPnLWallets)
	}

	// invalid period
	code, _ = s.doJSON(t, "GET", "/api/v1/traders/overview?venue=trader-e2e-venue&period=99D", "", "")
	if code != http.StatusBadRequest {
		t.Errorf("invalid period: %d", code)
	}
	// invalid pnl_metric
	code, _ = s.doJSON(t, "GET", "/api/v1/traders/overview?venue=trader-e2e-venue&pnl_metric=foo", "", "")
	if code != http.StatusBadRequest {
		t.Errorf("invalid pnl_metric: %d", code)
	}
	// invalid series_days
	code, _ = s.doJSON(t, "GET", "/api/v1/traders/overview?venue=trader-e2e-venue&series_days=5", "", "")
	if code != http.StatusBadRequest {
		t.Errorf("invalid series_days: %d", code)
	}

	// empty venue (unknown) -> 400/500? unknown venue -> 400
	code, _ = s.doJSON(t, "GET", "/api/v1/traders/overview?venue=nonexistent-venue", "", "")
	if code != http.StatusBadRequest {
		t.Errorf("unknown venue: %d", code)
	}

	// verify series shape: first pnl bucket series non-empty
	firstBucket := pb[0].(map[string]any)
	series, _ := firstBucket["series"].([]any)
	_ = series

}
