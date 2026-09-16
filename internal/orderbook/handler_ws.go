package orderbook

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 512
)

type WsMessage struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

type WsHub struct {
	clients    map[*WsClient]bool
	register   chan *WsClient
	unregister chan *WsClient
	mu         sync.RWMutex
}

type WsClient struct {
	hub          *WsHub
	conn         *websocket.Conn
	send         chan []byte
	service      *Service
	venueID      uuid.UUID
	instrumentID uuid.UUID
	depth        int
	cancel       func()
}

func NewWsHub() *WsHub {
	return &WsHub{
		clients:    make(map[*WsClient]bool),
		register:   make(chan *WsClient),
		unregister: make(chan *WsClient),
	}
}

func (h *WsHub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()
		}
	}
}

func (h *WsHub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

func (c *WsClient) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
		if c.cancel != nil {
			c.cancel()
		}
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("orderbook ws: read error: %v", err)
			}
			break
		}

		if string(message) == "ping" {
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PongMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *WsClient) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)
			w.Close()

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *WsClient) sendSnapshot(snapshot *OrderBookSnapshot) {
	msg := WsMessage{
		Type: "snapshot",
		Data: snapshot,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("orderbook ws: marshal error: %v", err)
		return
	}

	select {
	case c.send <- data:
	default:
		log.Printf("orderbook ws: client send buffer full, dropping message")
	}
}
