package hyperliquid

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func testLeaderboardServer(t *testing.T, body string, status int) (*Client, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/Mainnet/leaderboard" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	c := NewClient("http://unused", 0, 0)
	c.statsBaseURL = srv.URL
	return c, srv.Close
}

// DISC-U-01: pinned upstream fixture parses with lowercase addresses,
// month window mapped, ROI kept as upstream fraction.
func TestFetchLeaderboard_PinnedFixture(t *testing.T) {
	raw, err := os.ReadFile("testdata/leaderboard_sample.json")
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	// Strip the _meta helper key the sampler added.
	c, done := testLeaderboardServer(t, string(raw), http.StatusOK)
	defer done()

	rows, err := c.FetchLeaderboard(context.Background())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(rows) != 5 {
		t.Fatalf("want 5 rows, got %d", len(rows))
	}
	r0 := rows[0]
	if r0.Address != "0x85ecf584f25db6f146718b86d493e33c5af72052" {
		t.Errorf("address: %q", r0.Address)
	}
	m, ok := r0.Windows["month"]
	if !ok || m.PnL == nil || *m.PnL < 900000 {
		t.Errorf("month window: %+v", m)
	}
	if m.ROI == nil || *m.ROI > 1 {
		t.Errorf("ROI must stay a fraction, got %+v", m.ROI)
	}
	if r0.AccountValue == nil || *r0.AccountValue <= 0 {
		t.Errorf("account value: %+v", r0.AccountValue)
	}
}

// DISC-U-02/03 + BE-006: bad rows are skipped, incomplete rows kept.
func TestFetchLeaderboard_SkipsBadRows(t *testing.T) {
	body := `{"leaderboardRows": [
		{"ethAddress": "0xZZZ-not-hex", "accountValue": "1",
		 "windowPerformances": [["month", {"pnl": "1", "roi": "0.1", "vlm": "2"}]]},
		{"ethAddress": "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "accountValue": "badnum",
		 "windowPerformances": [["month", {"pnl": "oops", "roi": "", "vlm": ""}]]},
		{"ethAddress": "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "accountValue": "5",
		 "windowPerformances": [["broken"], ["month", {"pnl": "7", "roi": "0.2", "vlm": "8"}]]},
		{"ethAddress": "0xcccccccccccccccccccccccccccccccccccccccc", "accountValue": "9"}
	]}`
	c, done := testLeaderboardServer(t, body, http.StatusOK)
	defer done()

	rows, err := c.FetchLeaderboard(context.Background())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("malformed row must be skipped, want 3, got %d", len(rows))
	}
	if rows[0].AccountValue != nil || rows[0].Windows["month"].PnL != nil {
		t.Errorf("unparseable numbers must be nil, not zero: %+v", rows[0])
	}
	if rows[1].Windows["month"].PnL == nil || *rows[1].Windows["month"].PnL != 7 {
		t.Errorf("broken entry skipped, good entry kept: %+v", rows[1].Windows)
	}
	if len(rows[2].Windows) != 0 {
		t.Errorf("missing windows must yield empty map: %+v", rows[2].Windows)
	}
}

// BE-031: schema drift fails safe (hard error, caller keeps old data).
func TestFetchLeaderboard_SchemaDrift(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		status int
	}{
		{"missing rows key", `{"somethingElse": []}`, http.StatusOK},
		{"not json", `definitely not json`, http.StatusOK},
		{"upstream 429", `rate limited`, http.StatusTooManyRequests},
		{"upstream 500", `oops`, http.StatusInternalServerError},
	}
	for _, tc := range cases {
		c, done := testLeaderboardServer(t, tc.body, tc.status)
		if _, err := c.FetchLeaderboard(context.Background()); err == nil {
			t.Errorf("%s: expected hard error", tc.name)
		}
		done()
	}
}
