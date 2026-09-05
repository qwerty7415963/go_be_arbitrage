package ws

import (
	"sync"
)

type Message struct {
	Type    string      `json:"type"`
	Channel string      `json:"channel"`
	Payload interface{} `json:"payload"`
}

type Client struct {
	hub      *Hub
	conn     interface{}
	send     chan []byte
	channels map[string]bool
	mu       sync.Mutex
}

type Hub struct {
	clients    map[*Client]bool
	broadcast  chan []byte
	register   chan *Client
	unregister chan *Client
	mu         sync.RWMutex
}

func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan []byte, 256),
		register:   make(chan *Client),
		unregister: make(chan *Client),
	}
}

func (h *Hub) Run() {
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

		case message := <-h.broadcast:
			h.mu.RLock()
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					close(client.send)
					delete(h.clients, client)
				}
			}
			h.mu.RUnlock()
		}
	}
}

func (h *Hub) Broadcast(message []byte) {
	h.broadcast <- message
}

func (h *Hub) Register(client *Client) {
	h.register <- client
}

func (h *Hub) Unregister(client *Client) {
	h.unregister <- client
}

func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

func NewClient(hub *Hub, conn interface{}) *Client {
	return &Client{
		hub:      hub,
		conn:     conn,
		send:     make(chan []byte, 256),
		channels: make(map[string]bool),
	}
}

func (c *Client) Subscribe(channel string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.channels[channel] = true
}

func (c *Client) Unsubscribe(channel string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.channels, channel)
}

func (c *Client) IsSubscribed(channel string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.channels[channel]
}
