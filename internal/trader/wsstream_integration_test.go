//go:build integration

package trader

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/qwerty7415963/go_be_arbitrage/internal/hyperliquid"
)

// fakeWSServer scripts a Hyperliquid-like trades feed: per-connection
// scripted frames plus abrupt closes to exercise reconnects.
type fakeWSServer struct {
	t    *testing.T
	mu   sync.Mutex
	subs [][]string // subscribed coins per connection (1-indexed by order)

	// onSubscribed runs after a subscribe is recorded; return true to kill
	// the connection right after (abrupt close).
	onSubscribed func(connIdx int, send func(v any)) bool
	// frames2 are sent on the second connection (post-reconnect).
	frames2 []string
	done    chan struct{}
}

func (f *fakeWSServer) handler(w http.ResponseWriter, r *http.Request) {
	up := websocket.Upgrader{}
	conn, err := up.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	f.mu.Lock()
	f.subs = append(f.subs, nil)
	idx := len(f.subs)
	f.mu.Unlock()

	send := func(v any) {
		_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if err := conn.WriteJSON(v); err != nil {
			f.t.Logf("server write: %v", err)
		}
	}
	go func() {
		<-f.done
		conn.Close()
	}()
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var msg struct {
			Method       string `json:"method"`
			Subscription struct {
				Type string `json:"type"`
				Coin string `json:"coin"`
			} `json:"subscription"`
		}
		if json.Unmarshal(raw, &msg) != nil || msg.Method != "subscribe" {
			continue
		}
		kill := false
		f.mu.Lock()
		f.subs[idx-1] = append(f.subs[idx-1], msg.Subscription.Coin)
		if f.onSubscribed != nil {
			kill = f.onSubscribed(idx, send)
		}
		var extra []string
		if idx == 2 {
			extra = f.frames2
		}
		f.mu.Unlock()
		for _, frame := range extra {
			send(json.RawMessage(frame))
		}
		if kill {
			conn.Close()
			return
		}
	}
}

func tradeFrame(coin string, at time.Time, pairs ...[2]string) string {
	type leg struct {
		Coin  string   `json:"coin"`
		Side  string   `json:"side"`
		Px    string   `json:"px"`
		Sz    string   `json:"sz"`
		Time  int64    `json:"time"`
		Hash  string   `json:"hash"`
		Tid   int64    `json:"tid"`
		Users []string `json:"users"`
	}
	legs := make([]leg, 0, len(pairs))
	for i, p := range pairs {
		legs = append(legs, leg{coin, "B", "1", "1", at.UnixMilli(), "h", int64(i + 1), []string{p[0], p[1]}})
	}
	raw, _ := json.Marshal(map[string]any{"channel": "trades", "data": legs})
	return string(raw)
}

func wsURL(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

func waitFor(t *testing.T, timeout time.Duration, msg string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("timeout: %s", msg)
}

// WS-I-01/02/04: socket → harvest → registry; duplicates idempotent;
// abrupt close → reconnect + resubscribe, harvesting resumes.
func TestWSStream_Reconnect(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool := testPool(t)
	t.Cleanup(pool.Close)
	repo := NewRepository(pool)
	venueID := testVenue(t, pool)

	a1, a2 := testAddr(), testAddr()
	c1, c2 := testAddr(), testAddr()
	cleanupAddrs(t, pool, venueID, a1, a2, c1, c2)
	now := time.Now().UTC().Truncate(time.Second)

	server := &fakeWSServer{t: t, done: make(chan struct{})}
	server.onSubscribed = func(idx int, send func(v any)) bool {
		if idx != 1 {
			return false
		}
		frame := tradeFrame("BTC", now, [2]string{a1, a2})
		send(json.RawMessage(frame))
		send(json.RawMessage(frame)) // duplicate delivery (BE-004)
		return true                  // abrupt close after sending
	}
	server.frames2 = []string{tradeFrame("BTC", now.Add(time.Second), [2]string{c1, c2})}
	srv := httptest.NewServer(http.HandlerFunc(server.handler))
	defer srv.Close()
	defer close(server.done)

	harvest := NewWSHarvestService(repo, venueID, 100, 50*time.Millisecond)
	forward := func(events []hyperliquid.WSTradeEvent) { harvest.Submit(AdaptWSBatch(events)) }
	stream := hyperliquid.NewTradeStream(wsURL(t, srv), 10, forward)
	if err := stream.SetCoins([]string{"BTC"}); err != nil {
		t.Fatalf("coins: %v", err)
	}
	go harvest.Start(ctx)
	go func() { _ = stream.Run(ctx) }()

	has := func(addr string) bool {
		r, err := repo.GetRegistry(ctx, venueID, addr)
		return err == nil && r.DiscoverySource == SourceWSTrade
	}
	waitFor(t, 20*time.Second, "conn-1 wallets harvested (WS-I-01)",
		func() bool { return has(a1) && has(a2) })

	// Reconnect proof: a second connection that resubscribed (WS-I-04).
	// The manager backs off ~1s before redialing, so poll for it.
	waitFor(t, 20*time.Second, "manager reconnected after abrupt close (WS-I-04)", func() bool {
		server.mu.Lock()
		defer server.mu.Unlock()
		return len(server.subs) >= 2
	})
	waitFor(t, 20*time.Second, "post-reconnect wallets harvested (WS-I-04)",
		func() bool { return has(c1) && has(c2) })

	// Duplicate delivery stayed idempotent with max trade time (WS-I-02).
	for _, a := range []string{a1, a2} {
		r, err := repo.GetRegistry(ctx, venueID, a)
		if err != nil || r.LastTradeAt == nil || !r.LastTradeAt.Equal(now) {
			t.Errorf("dedupe/time %s: %+v %v", a, r, err)
		}
	}
	if st := stream.Stats(); st.Reconnects < 1 {
		t.Errorf("reconnect counter: %+v", st)
	}
}

// WS-I-06: burst of 10k events lands completely with bounded state.
func TestWSStream_Burst(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool := testPool(t)
	t.Cleanup(pool.Close)
	repo := NewRepository(pool)
	venueID := testVenue(t, pool)

	const unique = 2000
	addrs := make([]string, unique)
	for i := range addrs {
		addrs[i] = testAddr()
	}
	t.Cleanup(func() {
		for _, a := range addrs {
			_, _ = pool.Exec(context.Background(),
				`DELETE FROM trader_registry WHERE venue_id = $1 AND wallet_address = $2`, venueID, a)
		}
	})
	now := time.Now().UTC().Truncate(time.Second)
	var frames []string
	for i := 0; i < unique; i += 2 {
		frames = append(frames, tradeFrame("BTC", now,
			[2]string{addrs[i], addrs[i+1]}, [2]string{addrs[i], addrs[i+1]},
			[2]string{addrs[i], addrs[i+1]}, [2]string{addrs[i], addrs[i+1]},
			[2]string{addrs[i], addrs[i+1]}))
	}

	server := &fakeWSServer{t: t, done: make(chan struct{}), frames2: frames}
	srv := httptest.NewServer(http.HandlerFunc(server.handler))
	defer srv.Close()
	defer close(server.done)

	harvest := NewWSHarvestService(repo, venueID, 500, 50*time.Millisecond)
	forward := func(events []hyperliquid.WSTradeEvent) { harvest.Submit(AdaptWSBatch(events)) }
	stream := hyperliquid.NewTradeStream(wsURL(t, srv), 10, forward)
	if err := stream.SetCoins([]string{"BTC"}); err != nil {
		t.Fatalf("coins: %v", err)
	}
	// frames2 only fire on conn 2; force a reconnect by closing conn 1
	// without frames: point onSubscribed at conn 1 to kill immediately.
	server.onSubscribed = func(idx int, send func(v any)) bool { return idx == 1 }
	go harvest.Start(ctx)
	go func() { _ = stream.Run(ctx) }()

	waitFor(t, 45*time.Second, "burst wallets harvested (WS-I-06)", func() bool {
		var n int
		_ = pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM trader_registry WHERE venue_id = $1 AND discovery_source = 'ws_trade'`,
			venueID).Scan(&n)
		return n >= unique
	})
	if st := harvest.Stats(); st.Pending > 5000 {
		t.Errorf("pending unbounded: %+v", st)
	}
}
