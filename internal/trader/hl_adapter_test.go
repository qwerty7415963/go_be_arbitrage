package trader

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/qwerty7415963/go_be_arbitrage/internal/hyperliquid"
)

func TestHLFillAdapter_Mapping(t *testing.T) {
	body := `[
		{"coin": "BTC", "px": "100", "sz": "1", "side": "B", "time": 1727745600000,
		 "startPosition": "0", "dir": "Open Long", "closedPnl": "0", "hash": "h1",
		 "oid": 1, "tid": 11, "fee": "0.1", "feeToken": "USDC"},
		{"coin": "ETH", "px": "50", "sz": "2", "side": "A", "time": 1727745660000,
		 "startPosition": "2", "dir": "Close Long", "closedPnl": "5", "hash": "h2",
		 "oid": 2, "tid": 12, "fee": "bad-fee", "feeToken": "USDC"},
		{"coin": "BTC", "px": "nope", "sz": "1", "side": "B", "time": 1727745720000,
		 "startPosition": "0", "dir": "Open Long", "closedPnl": "0", "hash": "h3",
		 "oid": 3, "tid": 13, "fee": "0", "feeToken": "USDC"},
		{"coin": "SOL", "px": "10", "sz": "1", "side": "X", "time": 1727745780000,
		 "startPosition": "0", "dir": "?", "closedPnl": "0", "hash": "h4",
		 "oid": 4, "tid": 14, "fee": "0", "feeToken": "USDC"}
	]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	adapter := HLFillAdapter{C: hyperliquid.NewClient(srv.URL, 0, 0)}

	fills, truncated, err := adapter.FetchTraderFills(context.Background(),
		"0xabc", 1727745500000, 1727745900000)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if truncated {
		t.Error("small page must not be truncated")
	}
	if len(fills) != 2 {
		t.Fatalf("want 2 mapped (bad qty + bad side skipped), got %d", len(fills))
	}
	if !fills[0].Buy || fills[0].Market != "BTC" || fills[0].Tid != 11 {
		t.Errorf("buy mapping: %+v", fills[0])
	}
	if fills[1].Buy || fills[1].Fee != 0 {
		t.Errorf("sell mapping + fee fallback: %+v", fills[1])
	}
	if fills[1].ClosedPnL != 5 {
		t.Errorf("closedPnl: %+v", fills[1])
	}
}

func TestHLPortfolioAdapter_SkipsBadValues(t *testing.T) {
	body := `[["day", {"accountValueHistory": [[1727745600000, "100.5"], [1727749200000, "oops"], [0, "5"]]}]]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	adapter := HLPortfolioAdapter{C: hyperliquid.NewClient(srv.URL, 0, 0)}

	got, err := adapter.FetchPortfolio(context.Background(), "0xabc")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(got["day"]) != 1 || got["day"][0].Value != 100.5 {
		t.Errorf("bad values skipped: %+v", got["day"])
	}
}
