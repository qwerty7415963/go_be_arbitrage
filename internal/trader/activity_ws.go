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

// ActivityWSHandler upgrades GET /traders/ws and pumps realtime ActivityFill
// frames for one watched wallet (DETAIL-PLAN A8, M3 testability).
type ActivityWSHandler struct {
	hub      *ActivityHub
	upgrader websocket.Upgrader
}

func NewActivityWSHandler(hub *ActivityHub) *ActivityWSHandler {
	if hub == nil {
		hub = NewActivityHub()
	}
	return &ActivityWSHandler{hub: hub, upgrader: activityWSUpgrader}
}

// Hub exposes the watched set (wiring + tests).
func (h *ActivityWSHandler) Hub() *ActivityHub { return h.hub }

// ServeWS godoc
// @Summary      Trader activity stream (public WS)
// @Description  Realtime fill-level activity for one watched wallet (DETAIL-PLAN §4.2). Query wallet=0x... (lowercased server-side). Server sends {type:subscribed} on connect, then {type:activity} per fill; client {type:ping} gets {type:pong}. Reconnect/resubscribe is the FE's job. No auth (public read).
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
	client := h.hub.Subscribe(wallet)
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
	defer h.hub.Unsubscribe(client)
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
