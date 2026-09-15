package exchange

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// WSClientConfig holds configuration for a WebSocket client
type WSClientConfig struct {
	ReadLimit       int64
	PingInterval    time.Duration
	PongTimeout     time.Duration
	MaxReconnect    int
	InitialBackoff  time.Duration
	MaxBackoff      time.Duration
	BackoffMultiplier float64
}

// DefaultWSClientConfig returns sensible defaults
func DefaultWSClientConfig() WSClientConfig {
	return WSClientConfig{
		ReadLimit:         32 * 1024 * 1024, // 32MB
		PingInterval:      30 * time.Second,
		PongTimeout:       10 * time.Second,
		MaxReconnect:      10,
		InitialBackoff:    1 * time.Second,
		MaxBackoff:        60 * time.Second,
		BackoffMultiplier: 2.0,
	}
}

// WSClient manages a WebSocket connection with automatic reconnection
type WSClient struct {
	url        string
	config     WSClientConfig
	conn       *websocket.Conn
	mu         sync.RWMutex
	connected  bool
	stopCh     chan struct{}
	onMessage  func([]byte)
	onConnect  func()
	onDisconnect func()
}

// NewWSClient creates a new WebSocket client
func NewWSClient(url string, config WSClientConfig) *WSClient {
	return &WSClient{
		url:    url,
		config: config,
		stopCh: make(chan struct{}),
	}
}

// SetCallbacks sets event handlers
func (c *WSClient) SetCallbacks(onMessage func([]byte), onConnect func(), onDisconnect func()) {
	c.onMessage = onMessage
	c.onConnect = onConnect
	c.onDisconnect = onDisconnect
}

// Connect establishes the WebSocket connection
func (c *WSClient) Connect(ctx context.Context) error {
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.DialContext(ctx, c.url, http.Header{})
	if err != nil {
		return fmt.Errorf("websocket dial: %w", err)
	}

	conn.SetReadLimit(c.config.ReadLimit)

	c.mu.Lock()
	c.conn = conn
	c.connected = true
	c.mu.Unlock()

	if c.onConnect != nil {
		c.onConnect()
	}

	go c.readLoop()
	go c.pingLoop()

	return nil
}

// Disconnect gracefully closes the connection
func (c *WSClient) Disconnect() {
	close(c.stopCh)

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		c.conn.Close()
	}
	c.connected = false
}

// IsConnected returns the connection state
func (c *WSClient) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

// readLoop reads messages from the WebSocket
func (c *WSClient) readLoop() {
	defer func() {
		c.mu.Lock()
		c.connected = false
		c.mu.Unlock()

		if c.onDisconnect != nil {
			c.onDisconnect()
		}
	}()

	for {
		select {
		case <-c.stopCh:
			return
		default:
		}

		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				return
			}
			log.Printf("ws read error: %v", err)
			return
		}

		if c.onMessage != nil {
			c.onMessage(message)
		}
	}
}

// pingLoop sends periodic pings
func (c *WSClient) pingLoop() {
	ticker := time.NewTicker(c.config.PingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.mu.RLock()
			conn := c.conn
			c.mu.RUnlock()

			if conn != nil {
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					log.Printf("ws ping error: %v", err)
					return
				}
			}
		}
	}
}

// Reconnect attempts to reconnect with exponential backoff
func (c *WSClient) Reconnect(ctx context.Context) error {
	backoff := c.config.InitialBackoff

	for attempt := 0; attempt < c.config.MaxReconnect; attempt++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}

		log.Printf("ws reconnect attempt %d/%d", attempt+1, c.config.MaxReconnect)

		err := c.Connect(ctx)
		if err == nil {
			return nil
		}

		log.Printf("ws reconnect failed: %v", err)
		backoff = time.Duration(math.Min(
			float64(backoff)*c.config.BackoffMultiplier,
			float64(c.config.MaxBackoff),
		))
	}

	return fmt.Errorf("max reconnect attempts reached")
}

// WSMessage represents a generic WebSocket message with event type
type WSMessage struct {
	E string `json:"e"` // Event type
}

// DetectMessageType returns the event type from a raw message
func DetectMessageType(data []byte) string {
	var msg WSMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return ""
	}
	return msg.E
}
