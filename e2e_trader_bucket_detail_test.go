//go:build e2e

package e2e

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/qwerty7415963/go_be_arbitrage/internal/trader"
)

func TestE2E_BucketDetail(t *testing.T) {
	s := setupTraderSuite(t)
	ctx := context.Background()
	repo := trader.NewRepository(s.db)

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
		eq := pnl * 10
		day := time.Now().UTC().Truncate(24 * time.Hour)
		if err := repo.UpsertEquityDaily(ctx, s.venueID, addr, trader.EquityDay{
			Date: day, StartEquity: &eq, EndEquity: &eq, PeakEquity: &eq,
		}); err != nil {
			t.Fatalf("equity: %v", err)
		}
		rpnl := pnl / 2
		if err := repo.UpsertDailyStats(ctx, &trader.DailyStats{
			VenueID: s.venueID, WalletAddress: addr, StatDate: day,
			RealizedPnL: &rpnl, TradeCount: tc, LongCount: lc, ShortCount: sc,
			Volume: &vol,
		}); err != nil {
			t.Fatalf("daily: %v", err)
		}
		snap := &trader.PositionSnapshot{
			Positions: []trader.Position{
				{Coin: "BTC", Side: "LONG", Size: 0.5, EntryPrice: fptr64(60000), MarkPrice: fptr64(63000), PositionValue: fptr64(31500)},
			},
			AccountValue:    &eq,
			TotalNtlPos:     &vol,
			TotalMarginUsed: fptr64(800),
			AsOf:            time.Now().UTC(),
		}
		if err := repo.ReplacePositions(ctx, s.venueID, addr, snap); err != nil {
			t.Fatalf("positions: %v", err)
		}
		tradeDay := time.Now().UTC().Truncate(24 * time.Hour)
		trades := []trader.CompletedTrade{
			{Market: "BTC", Long: true, OpenTime: tradeDay.Add(1 * time.Hour), CloseTime: tradeDay.Add(2 * time.Hour), Volume: vol, PnL: pnl / 2, Fees: 10, Fills: 2},
		}
		if err := repo.ReplaceTradesForDay(ctx, s.venueID, addr, tradeDay, trades); err != nil {
			t.Fatalf("trades: %v", err)
		}
	}

	// bucket detail
	code, resp := s.doJSON(t, "GET", "/api/v1/traders/overview/buckets/pnl/profitable?venue=trader-e2e-venue", "", "")
	if code != http.StatusOK {
		t.Fatalf("bucket detail: %d %v", code, resp)
	}
	data := resp["data"].(map[string]any)
	if data["kind"] != "pnl" || data["bucket_id"] != "profitable" {
		t.Errorf("detail identity: %v %v", data["kind"], data["bucket_id"])
	}
	if data["wallet_count"].(float64) != 3 {
		t.Errorf("wallet_count: %v", data["wallet_count"])
	}
	if _, ok := data["positioning"].([]any); !ok {
		t.Errorf("positioning missing")
	}
	if _, ok := data["markets_top"].([]any); !ok {
		t.Errorf("markets_top missing")
	}

	// bucket wallets
	code, resp = s.doJSON(t, "GET", "/api/v1/traders/overview/buckets/pnl/profitable/wallets?venue=trader-e2e-venue", "", "")
	if code != http.StatusOK {
		t.Fatalf("bucket wallets: %d %v", code, resp)
	}
	rows := resp["data"].([]any)
	if len(rows) == 0 {
		t.Errorf("wallets empty")
	}
	if resp["meta"] == nil {
		t.Errorf("meta missing")
	}

	// bucket markets
	code, resp = s.doJSON(t, "GET", "/api/v1/traders/overview/buckets/pnl/profitable/markets?venue=trader-e2e-venue", "", "")
	if code != http.StatusOK {
		t.Fatalf("bucket markets: %d %v", code, resp)
	}
	mrows := resp["data"].([]any)
	if len(mrows) == 0 {
		t.Errorf("markets empty")
	}

	// account kind path
	code, resp = s.doJSON(t, "GET", "/api/v1/traders/overview/buckets/account/small?venue=trader-e2e-venue", "", "")
	// small bucket may be empty (all wallets have high equity) -> 404 acceptable; but ensure not 500
	if code != http.StatusOK && code != http.StatusNotFound {
		t.Errorf("account bucket detail: %d %v", code, resp)
	}

	// invalid
	code, _ = s.doJSON(t, "GET", "/api/v1/traders/overview/buckets/pnl/bogus?venue=trader-e2e-venue", "", "")
	if code != http.StatusBadRequest {
		t.Errorf("invalid bucket: %d", code)
	}
}

func fptr64(v float64) *float64 { return &v }
