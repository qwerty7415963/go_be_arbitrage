package exchange

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// hlMetaFixture mirrors the live metaAndAssetCtxs envelope: outer[0] is an
// object with a "universe" array (market defs), outer[1] is the aligned
// array of asset contexts.
const hlMetaFixture = `[
  {
    "universe": [
      {"name": "BTC", "szDecimals": 5, "maxLeverage": 50},
      {"name": "ETH", "szDecimals": 4, "maxLeverage": 50},
      {"name": "DELISTED", "szDecimals": 1, "isDelisted": true},
      {"name": "", "szDecimals": 1}
    ],
    "marginTables": [],
    "collateralToken": 0
  },
  [
    {"funding": "0.0000125", "openInterest": "100.5", "markPx": "67000.5", "oraclePx": "67001.0"},
    {"funding": "-0.0000031", "openInterest": "200.25", "markPx": "3500.0", "oraclePx": "3500.5"},
    {"funding": "0.0", "openInterest": "0", "markPx": "1", "oraclePx": "1"},
    {"funding": "0.0", "openInterest": "0", "markPx": "1", "oraclePx": "1"}
  ]
]`

func hlTestServer(t *testing.T, body string, status int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/info" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
}

// FA-U-08: valid fixture parses perps with funding/mark/index/OI.
func TestHyperliquidAdapter_FetchAllFunding_Valid(t *testing.T) {
	srv := hlTestServer(t, hlMetaFixture, http.StatusOK)
	defer srv.Close()

	a := NewHyperliquidAdapterWithURL(srv.URL)
	if a.GetVenueCode() != "hyperliquid" {
		t.Errorf("venue code: %s", a.GetVenueCode())
	}
	if a.GetFundingInterval() != 3600 {
		t.Errorf("interval: %d", a.GetFundingInterval())
	}

	got, err := a.FetchAllFunding(context.Background())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 markets (delisted + blank skipped), got %d", len(got))
	}

	btc := got[0]
	if btc.Symbol != "BTC" || btc.BaseAsset != "BTC" || btc.QuoteAsset != "USDT" {
		t.Errorf("BTC identity: %+v", btc)
	}
	if btc.FundingRate != "0.0000125" || btc.MarkPrice != "67000.5" ||
		btc.IndexPrice != "67001.0" || btc.OI != "100.5" {
		t.Errorf("BTC fields: %+v", btc)
	}
	if time.Since(btc.ObservedAt) > time.Minute {
		t.Errorf("ObservedAt not now: %v", btc.ObservedAt)
	}

	eth := got[1]
	if eth.Symbol != "ETH" || eth.FundingRate != "-0.0000031" {
		t.Errorf("ETH fields: %+v", eth)
	}
}

// FA-U-09 is covered above (DELISTED skipped); FA-U-10: length mismatch
// tolerated without crash.
func TestHyperliquidAdapter_FetchAllFunding_LengthMismatch(t *testing.T) {
	srv := hlTestServer(t,
		`[{"universe": [{"name": "BTC"}, {"name": "ETH"}, {"name": "SOL"}]},
		  [{"funding": "0.1", "markPx": "1", "oraclePx": "1", "openInterest": "1"}]]`,
		http.StatusOK)
	defer srv.Close()

	a := NewHyperliquidAdapterWithURL(srv.URL)
	got, err := a.FetchAllFunding(context.Background())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(got) != 1 || got[0].Symbol != "BTC" {
		t.Errorf("expected only the aligned row, got %+v", got)
	}
}

// FA-U-12 regression: the live payload's meta object (with marginTables /
// collateralToken siblings) must parse, and an envelope without a
// universe array must surface an error.
func TestHyperliquidAdapter_FetchAllFunding_MetaEnvelope(t *testing.T) {
	// Excerpt of the real response: extra sibling keys are ignored.
	srv := hlTestServer(t,
		`[{"universe": [{"szDecimals": 5, "name": "BTC", "maxLeverage": 40, "marginTableId": 56},
		                {"szDecimals": 4, "name": "ETH", "maxLeverage": 25, "marginTableId": 55}],
		   "marginTables": [[56, {"description": "tiered 40x"}]],
		   "collateralToken": 0},
		  [{"funding": "0.0000040966", "openInterest": "36660.62764", "markPx": "82965.0", "oraclePx": "83006.0"},
		   {"funding": "-0.000001", "openInterest": "100", "markPx": "3000.0", "oraclePx": "3001.0"}]]`,
		http.StatusOK)
	defer srv.Close()

	a := NewHyperliquidAdapterWithURL(srv.URL)
	got, err := a.FetchAllFunding(context.Background())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(got) != 2 || got[0].Symbol != "BTC" || got[1].Symbol != "ETH" {
		t.Fatalf("markets: %+v", got)
	}
	if got[0].FundingRate != "0.0000040966" || got[0].MarkPrice != "82965.0" {
		t.Errorf("BTC fields: %+v", got[0])
	}

	noUniverse := hlTestServer(t, `[{"marginTables": []}, [{"funding": "1"}]]`, http.StatusOK)
	defer noUniverse.Close()
	b := NewHyperliquidAdapterWithURL(noUniverse.URL)
	if _, err := b.FetchAllFunding(context.Background()); err == nil {
		t.Error("expected error when universe is missing")
	}
}

// FA-U-11: HTTP error and bad JSON surface as errors.
func TestHyperliquidAdapter_FetchAllFunding_Errors(t *testing.T) {
	srv := hlTestServer(t, "busy", http.StatusTooManyRequests)
	defer srv.Close()

	a := NewHyperliquidAdapterWithURL(srv.URL)
	if _, err := a.FetchAllFunding(context.Background()); err == nil {
		t.Error("expected error on 429")
	}

	bad := hlTestServer(t, `{"not": "an array"}`, http.StatusOK)
	defer bad.Close()

	b := NewHyperliquidAdapterWithURL(bad.URL)
	if _, err := b.FetchAllFunding(context.Background()); err == nil {
		t.Error("expected error on malformed envelope")
	}
}
