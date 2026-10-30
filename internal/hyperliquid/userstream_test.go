package hyperliquid

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"
)

func TestUserStream_Constants(t *testing.T) {
	if MaxSubsPerConn != 30 {
		t.Fatalf("MaxSubsPerConn=%d", MaxSubsPerConn)
	}
	if MaxOutboundPerSec != 10 {
		t.Fatalf("MaxOutboundPerSec=%d", MaxOutboundPerSec)
	}
}

type fakeUSConn struct {
	mu      sync.Mutex
	written []any
	readCh  chan []byte
	errCh   chan error
	closed  bool
}

func newFakeUSConn() *fakeUSConn {
	return &fakeUSConn{readCh: make(chan []byte, 16), errCh: make(chan error, 1)}
}

func (c *fakeUSConn) ReadMessage() (int, []byte, error) {
	select {
	case b := <-c.readCh:
		return 1, b, nil
	case err := <-c.errCh:
		return 0, nil, err
	case <-time.After(200 * time.Millisecond):
		return 0, nil, nil
	}
}

func (c *fakeUSConn) WriteJSON(v any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.written = append(c.written, v)
	return nil
}

func (c *fakeUSConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}

func TestUserStream_SubscribeUnsubscribe(t *testing.T) {
	conn := newFakeUSConn()
	dials := 0
	dial := func(ctx context.Context, url string) (*fakeUSConn, error) {
		dials++
		return conn, nil
	}
	us := NewUserStream("wss://example/ws", nil, dial)
	// sink optional
	_ = 0

	ctx := context.Background()
	if err := us.Subscribe(ctx, "0xabc"); err != nil {
		t.Fatal(err)
	}
	if dials != 1 {
		t.Fatalf("dials=%d", dials)
	}
	us.mu.Lock()
	if !us.active["0xabc"] {
		t.Fatal("not active")
	}
	us.mu.Unlock()

	// check written frames
	conn.mu.Lock()
	n := len(conn.written)
	conn.mu.Unlock()
	if n < 2 {
		t.Fatalf("written=%d", n)
	}

	if err := us.Unsubscribe("0xabc"); err != nil {
		t.Fatal(err)
	}
	conn.mu.Lock()
	if !conn.closed {
		t.Fatal("conn not closed")
	}
	conn.mu.Unlock()
}

func TestUserStream_DispatchAndReconnect(t *testing.T) {
	conn := newFakeUSConn()
	dials := 0
	var mu sync.Mutex
	dial := func(ctx context.Context, url string) (*fakeUSConn, error) {
		mu.Lock()
		dials++
		mu.Unlock()
		c := newFakeUSConn()
		// share? use new conn each time
		// store latest
		latestConn = c
		return c, nil
	}
	_ = dial
	// We need access to latest conn; use global
	latestConn = conn

	// Use a single conn approach: dial returns same conn first, then new on reconnect
	type holder struct {
		mu    sync.Mutex
		conns []*fakeUSConn
	}
	h := &holder{}
	dial2 := func(ctx context.Context, url string) (*fakeUSConn, error) {
		c := newFakeUSConn()
		h.mu.Lock()
		h.conns = append(h.conns, c)
		h.mu.Unlock()
		return c, nil
	}

	receivedFills := make(chan Fill, 1)
	receivedOrders := make(chan WsOrderUpdate, 1)
	_ = receivedFills
	_ = receivedOrders

	us := NewUserStream("wss://example/ws", dial2)
	// inject a sink via a custom type
	type mySink struct {
		fills  chan Fill
		orders chan WsOrderUpdate
	}
	adapter := &usSinkAdapter{fills: receivedFills, orders: receivedOrders}
	us.Sink = adapter

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := us.Subscribe(ctx, "0xdef"); err != nil {
		t.Fatal(err)
	}
	// get first conn
	h.mu.Lock()
	first := h.conns[0]
	h.mu.Unlock()

	// push a userFills frame
	frame, _ := json.Marshal(map[string]any{
		"channel": "userFills",
		"data": map[string]any{
			"user":  "0xdef",
			"fills": []map[string]any{{"coin": "BTC", "side": "B", "sz": "1", "px": "50000", "time": time.Now().UnixMilli(), "dir": "Open Long", "closedPnl": "0", "tid": 1}},
		},
	})
	first.readCh <- frame

	select {
	case f := <-receivedFills:
		if f.Coin != "BTC" {
			t.Errorf("fill coin: %s", f.Coin)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("fill not received")
	}

	// push orderUpdates frame
	frame2, _ := json.Marshal(map[string]any{
		"channel": "orderUpdates",
		"data":    []map[string]any{{"order": map[string]any{"coin": "BTC", "side": "B", "limitPx": "50000", "sz": "1", "origSz": "1", "oid": 7, "timestamp": time.Now().UnixMilli(), "orderType": "limit"}, "status": "open", "statusTimestamp": time.Now().UnixMilli()}},
	})
	first.readCh <- frame2
	select {
	case o := <-receivedOrders:
		if o.Order.Oid != 7 {
			t.Errorf("order oid: %d", o.Order.Oid)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("order not received")
	}

	// trigger reconnect
	first.errCh <- context.DeadlineExceeded
	time.Sleep(1500 * time.Millisecond)
	h.mu.Lock()
	n := len(h.conns)
	h.mu.Unlock()
	if n < 2 {
		t.Fatalf("reconnect did not dial, conns=%d", n)
	}
}

var latestConn *fakeUSConn

type usSinkAdapter struct {
	fills  chan Fill
	orders chan WsOrderUpdate
}

func (a *usSinkAdapter) OnRawUserFill(addr string, fill Fill) {
	select {
	case a.fills <- fill:
	default:
	}
}

func (a *usSinkAdapter) OnOrder(addr string, upd WsOrderUpdate) {
	select {
	case a.orders <- upd:
	default:
	}
}
