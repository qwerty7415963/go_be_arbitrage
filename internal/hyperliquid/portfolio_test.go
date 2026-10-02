package hyperliquid

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// EQ-U-01: portfolio shape parses per window; bad entries skipped.
func TestFetchPortfolio(t *testing.T) {
	body := `[
		["day", {"accountValueHistory": [[1727745600000, "100.5"], [1727749200000, "120.25"]],
			"pnlHistory": [], "vlm": "1"}],
		["week", {"accountValueHistory": [[1727745600000, "bad"]]}],
		["broken-entry"],
		["month", {"accountValueHistory": "oops"}]
	]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]string
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req["type"] != "portfolio" {
			t.Errorf("request: %v %+v", err, req)
		}
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	c := NewClient(srv.URL, 0, 0)

	got, err := c.FetchPortfolio(context.Background(), "0xabc")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	day := got["day"]
	if len(day) != 2 || day[0].Time != 1727745600000 || day[1].Value != "120.25" {
		t.Errorf("day window: %+v", day)
	}
	// Raw fetch passes values through as strings (numeric validation happens
	// in the trader adapter); only structurally broken entries are dropped.
	if len(got["week"]) != 1 {
		t.Errorf("week kept raw: %+v", got["week"])
	}
	if len(got) != 2 {
		t.Errorf("only clean entries kept: %v", got)
	}
}
