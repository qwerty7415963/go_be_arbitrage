package hyperliquid

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func portfolioServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]string
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req["type"] != "portfolio" {
			t.Errorf("request: %v %+v", err, req)
		}
		_, _ = w.Write([]byte(body))
	}))
}

// PS-U-01/02: both histories parse per window (base + perp); malformed
// pairs/windows are skipped.
func TestFetchPortfolio(t *testing.T) {
	body := `[
		["day", {"accountValueHistory": [[1727745600000, "100.5"], [1727749200000, "120.25"], [1727752800000, 5]],
			"pnlHistory": [[1727745600000, "10.5"], [1727749200000, 5], [1727752800000, "-5"]],
			"vlm": "1"}],
		["perpDay", {"accountValueHistory": [[1727745600000, "50"]],
			"pnlHistory": [[1727745600000, "1.25"]]}],
		["week", {"accountValueHistory": [], "pnlHistory": []}],
		["broken-entry"],
		["month", {"accountValueHistory": "oops"}]
	]`
	srv := portfolioServer(t, body)
	defer srv.Close()
	c := NewClient(srv.URL, 0, 0)

	got, err := c.FetchPortfolio(context.Background(), "0xabc")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	day := got["day"]
	if len(day.AccountValue) != 2 || day.AccountValue[0].Time != 1727745600000 || day.AccountValue[1].Value != "120.25" {
		t.Errorf("day account value: %+v", day.AccountValue)
	}
	// "bad-value" is still a string (numeric validation happens in the trader
	// adapter); the non-string pair is structurally invalid and dropped.
	if len(day.PnL) != 2 || day.PnL[0].Value != "10.5" || day.PnL[1].Value != "-5" {
		t.Errorf("day pnl: %+v", day.PnL)
	}
	perp := got["perpDay"]
	if len(perp.AccountValue) != 1 || len(perp.PnL) != 1 || perp.PnL[0].Value != "1.25" {
		t.Errorf("perpDay series: %+v", perp)
	}
	if got["week"].AccountValue == nil || got["week"].PnL == nil || len(got["week"].AccountValue) != 0 {
		t.Errorf("empty window must keep non-nil empty histories: %+v", got["week"])
	}
	if _, ok := got["month"]; ok {
		t.Errorf("structurally broken window must be skipped")
	}
	if _, ok := got["broken-entry"]; ok {
		t.Errorf("broken entry must be skipped")
	}
	if len(got) != 3 {
		t.Errorf("only clean windows kept: %v", got)
	}
}

// PS-U-03: null histories normalize to non-nil empty slices.
func TestFetchPortfolio_NilHistories(t *testing.T) {
	body := `[["day", {"accountValueHistory": null, "pnlHistory": null}]]`
	srv := portfolioServer(t, body)
	defer srv.Close()
	c := NewClient(srv.URL, 0, 0)

	got, err := c.FetchPortfolio(context.Background(), "0xabc")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	day := got["day"]
	if day.AccountValue == nil || day.PnL == nil {
		t.Fatalf("nil histories must map to empty slices: %+v", day)
	}
	if len(day.AccountValue) != 0 || len(day.PnL) != 0 {
		t.Errorf("want empty histories, got %+v", day)
	}
}
