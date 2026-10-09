package trader

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/qwerty7415963/go_be_arbitrage/internal/api"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

// activityWSUpgrader accepts cross-origin dials (public read, parity with
// internal/orderbook upgrader).
var activityWSUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// ActivityWSHandler upgrades GET /traders/ws and pumps realtime frames for
// one watched wallet (LIVE-CONTRACT v1.2 §2: wallet.* envelope; legacy
// {type:activity} path kept for the global harvest feed).
type ActivityWSHandler struct {
	hub      *ActivityHub
	watcher  *WatcherManager
	upgrader websocket.Upgrader
}

func NewActivityWSHandler(hub *ActivityHub) *ActivityWSHandler {
	if hub == nil {
		hub = NewActivityHub()
	}
	return &ActivityWSHandler{hub: hub, upgrader: activityWSUpgrader}
}

// WithWatcher attaches the per-address WalletWatcher set (WS-D). Nil keeps
// the hub-only surface (unit routers without upstream watchers).
func (h *ActivityWSHandler) WithWatcher(m *WatcherManager) *ActivityWSHandler {
	h.watcher = m
	return h
}

// Hub exposes the watched set (wiring + tests).
func (h *ActivityWSHandler) Hub() *ActivityHub { return h.hub }

// Watcher exposes the manager (wiring/tests; may be nil in hub-only contexts).
func (h *ActivityWSHandler) Watcher() *WatcherManager { return h.watcher }

// ServeWS godoc
// @Summary      Trader wallet stream (public WS)
// @Description  Realtime wallet events for one watched wallet (LIVE-CONTRACT v1.2 §2). Query wallet=0x... (lowercased server-side). Transport is GET /traders/ws?wallet= (unchanged); payloads use the wallet.* envelope for incremental updates (no full refetch): wallet.position.updated (REST bootstrap/resync only), wallet.fill.created, wallet.funding.created, wallet.order.updated, wallet.activity.created, wallet.state.updated, wallet.connection.updated (DISCONNECTED→RECONNECT→RESYNC→RECONCILE→LIVE). Server sends {type:subscribed} on connect, then wallet.* frames; legacy {type:activity} frames from the global harvest feed are still forwarded. Client {type:ping} gets {type:pong}. Existing backoff + hidden-suspend + ping/pong stay. No auth (public read).
// @Tags         traders
// @Produce      json
// @Param        wallet  query  string  true  "Watched wallet address (0x...)"
// @Success      200  {string}  string  "websocket upgrade"
// @Failure      400  {object}  api.Response{error=api.ErrorBody}  "invalid wallet"
// @Router       /api/v1/traders/ws [get]
func (h *ActivityWSHandler) ServeWS(c *gin.Context) {
	wallet, err := NormalizeAddress(c.Query("wallet"))
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid wallet address"))
		return
	}
	conn, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	var client *ActivityClient
	var onClose func()
	if h.watcher != nil {
		// Refcounted WalletWatcher: first subscriber creates the upstream
		// watcher (4 feeds + REST bootstrap); last unsubscribe tears it down.
		client = h.watcher.Subscribe(wallet)
		onClose = func() { h.watcher.Unsubscribe(client) }
	} else {
		client = h.hub.Subscribe(wallet)
		onClose = func() { h.hub.Unsubscribe(client) }
	}
	if msg, merr := json.Marshal(map[string]any{
		"type": "subscribed", "data": map[string]string{"wallet": wallet},
	}); merr == nil {
		select {
		case client.send <- msg:
		default:
		}
	}
	// Single owner for conn.Close + Unsubscribe: ServeWS owns both. The
	// pumps share a sync.Once close func so a read error (which unblocks
	// via conn close) and a write error (which must unblock the reader)
	// can each request the close without a double-close race.
	var closeOnce sync.Once
	closeConn := func() { closeOnce.Do(func() { _ = conn.Close() }) }
	defer closeConn()
	defer onClose()
	go h.writePump(conn, client, closeConn)
	h.readPump(conn, client, closeConn)
}

func (h *ActivityWSHandler) readPump(conn *websocket.Conn, client *ActivityClient, closeConn func()) {
	defer closeConn()
	conn.SetReadLimit(512)
	_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	})
	for {
		var msg struct {
			Type string `json:"type"`
		}
		if err := conn.ReadJSON(&msg); err != nil {
			return // close or protocol error: unsubscribe-on-close.
		}
		if msg.Type == "ping" {
			_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteJSON(map[string]string{"type": "pong"}); err != nil {
				return
			}
		}
	}
}

func (h *ActivityWSHandler) writePump(conn *websocket.Conn, client *ActivityClient, closeConn func()) {
	defer closeConn()
	for msg := range client.send {
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			return
		}
	}
}
