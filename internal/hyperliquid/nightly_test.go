//go:build nightly

// Nightly upstream contract checks: live Hyperliquid shape assertions.
// Read-only (no DB writes); fail loudly on upstream drift so daytime
// pipelines never silently ingest garbage. Run by ci-nightly.yml.
package hyperliquid

import (
	"context"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func nightlyCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 90*time.Second)
}

// NIGHTLY-01: leaderboard shape (rows, keys, all four windows).
func TestNightly_LeaderboardShape(t *testing.T) {
	if testing.Short() {
		t.Skip("nightly only")
	}
	ctx, cancel := nightlyCtx()
	defer cancel()
	c := NewClient("", 30*time.Second, 0)

	rows, err := c.FetchLeaderboard(ctx)
	if err != nil {
		t.Fatalf("leaderboard: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("empty board")
	}
	r0 := rows[0]
	if r0.Address == "" || r0.AccountValue == nil {
		t.Errorf("row keys: %+v", r0)
	}
	for _, w := range []string{"day", "week", "month", "allTime"} {
		found := false
		for _, r := range rows {
			if _, ok := r.Windows[w]; ok {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("window %s absent everywhere", w)
		}
	}
	t.Logf("board rows: %d", len(rows))
}

// NIGHTLY-02: WS trades frame shape (users pair present).
func TestNightly_WSFrame(t *testing.T) {
	if testing.Short() {
		t.Skip("nightly only")
	}
	ctx, cancel := nightlyCtx()
	defer cancel()

	dialer := websocket.Dialer{HandshakeTimeout: 15 * time.Second}
	conn, _, err := dialer.DialContext(ctx, DefaultWSURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(map[string]any{
		"method":       "subscribe",
		"subscription": map[string]any{"type": "trades", "coin": "BTC"},
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		events, handled, err := parseTradeFrame(raw)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if !handled || len(events) == 0 {
			continue
		}
		if len(events[0].Buyer) != 42 || len(events[0].Seller) != 42 {
			t.Fatalf("users pair: %+v", events[0])
		}
		t.Logf("coin=%s buyer=%.12s…", events[0].Coin, events[0].Buyer)
		return
	}
	t.Fatal("no trades frame within deadline")
}

// NIGHTLY-03: portfolio shape (8 windows with history pairs).
func TestNightly_PortfolioShape(t *testing.T) {
	if testing.Short() {
		t.Skip("nightly only")
	}
	ctx, cancel := nightlyCtx()
	defer cancel()
	c := NewClient("", 30*time.Second, 0)

	got, err := c.FetchPortfolio(ctx, "0x85ecf584f25db6f146718b86d493e33c5af72052")
	if err != nil {
		t.Fatalf("portfolio: %v", err)
	}
	for _, w := range []string{"day", "week", "month", "allTime"} {
		pts, ok := got[w]
		if !ok || len(pts) == 0 {
			t.Errorf("window %s missing/empty", w)
		}
	}
}

// NIGHTLY-04: meta universe non-empty and below the subscription cap.
func TestNightly_MetaUniverse(t *testing.T) {
	if testing.Short() {
		t.Skip("nightly only")
	}
	ctx, cancel := nightlyCtx()
	defer cancel()
	c := NewClient("", 30*time.Second, 0)

	coins, err := c.FetchPerpCoins(ctx)
	if err != nil {
		t.Fatalf("meta: %v", err)
	}
	if len(coins) == 0 {
		t.Fatal("empty universe")
	}
	if len(coins) > DefaultMaxCoins {
		t.Errorf("universe %d exceeds cap %d — raise cap before discovery refuses", len(coins), DefaultMaxCoins)
	}
	t.Logf("universe coins: %d", len(coins))
}
