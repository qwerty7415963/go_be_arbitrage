package trader

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func wsTestServer(h *ActivityWSHandler) *httptest.Server {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/v1/traders/ws", h.ServeWS)
	return httptest.NewServer(r)
}

func wsDial(t *testing.T, url, wallet string) *websocket.Conn {
	t.Helper()
	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	conn, resp, err := dialer.Dial("ws"+strings.TrimPrefix(url, "http")+"/api/v1/traders/ws?wallet="+wallet, nil)
	if err != nil {
		if resp != nil {
			t.Fatalf("dial: %v status=%d", err, resp.StatusCode)
		}
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func wsRead(t *testing.T, conn *websocket.Conn) map[string]any {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var msg map[string]any
	if err := conn.ReadJSON(&msg); err != nil {
		t.Fatalf("read: %v", err)
	}
	return msg
}

// WS-H-01 (M3): httptest + real WS dial: connect -> subscribed -> Publish -> receive.
func TestActivityWS_SubscribePublish(t *testing.T) {
	hub := NewActivityHub()
	h := NewActivityWSHandler(hub)
	srv := wsTestServer(h)
	defer srv.Close()

	wallet := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	conn := wsDial(t, srv.URL, wallet)

	msg := wsRead(t, conn)
	if msg["type"] != "subscribed" {
		t.Fatalf("want subscribed, got %v", msg)
	}
	if len(hub.WatchSet()) != 1 {
		t.Fatalf("watchset: %v", hub.WatchSet())
	}

	hub.Publish(wallet, ActivityFill{
		Coin: "BTC", Side: "BUY", Size: 0.1, Price: 60000,
		Time: time.Now().UTC(), Tid: 12345,
	})
	got := wsRead(t, conn)
	if got["type"] != "activity" {
		t.Fatalf("want activity, got %v", got)
	}
	raw, _ := json.Marshal(got["data"])
	var fill ActivityFill
	if err := json.Unmarshal(raw, &fill); err != nil {
		t.Fatalf("fill decode: %v", err)
	}
	if fill.Coin != "BTC" || fill.Side != "BUY" || fill.Tid != 12345 {
		t.Errorf("fill: %+v", fill)
	}
}

// WS-H-02: {"type":"ping"} -> {"type":"pong"}.
func TestActivityWS_PingPong(t *testing.T) {
	hub := NewActivityHub()
	h := NewActivityWSHandler(hub)
	srv := wsTestServer(h)
	defer srv.Close()

	conn := wsDial(t, srv.URL, "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	_ = wsRead(t, conn) // subscribed
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if err := conn.WriteJSON(map[string]string{"type": "ping"}); err != nil {
		t.Fatalf("ping: %v", err)
	}
	msg := wsRead(t, conn)
	if msg["type"] != "pong" {
		t.Fatalf("want pong, got %v", msg)
	}
}

// WS-H-03: close unsubscribes; invalid wallet -> 400, no upgrade.
func TestActivityWS_CloseUnsubscribes(t *testing.T) {
	hub := NewActivityHub()
	h := NewActivityWSHandler(hub)
	srv := wsTestServer(h)
	defer srv.Close()

	wallet := "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	conn := wsDial(t, srv.URL, wallet)
	_ = wsRead(t, conn) // subscribed
	if hub.SubscriberCount(wallet) != 1 {
		t.Fatalf("subs: %d", hub.SubscriberCount(wallet))
	}
	_ = conn.Close()
	deadline := time.Now().Add(5 * time.Second)
	for hub.SubscriberCount(wallet) != 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if hub.SubscriberCount(wallet) != 0 || len(hub.WatchSet()) != 0 {
		t.Errorf("close must unsubscribe: subs=%d watch=%v",
			hub.SubscriberCount(wallet), hub.WatchSet())
	}

	resp, err := http.Get(srv.URL + "/api/v1/traders/ws?wallet=not-an-address")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("invalid wallet must be 400, got %d", resp.StatusCode)
	}
}
