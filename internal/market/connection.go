package market

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

type ConnectionState string

const (
	ConnectionStateDisconnected ConnectionState = "DISCONNECTED"
	ConnectionStateConnecting   ConnectionState = "CONNECTING"
	ConnectionStateConnected    ConnectionState = "CONNECTED"
	ConnectionStateReconnecting ConnectionState = "RECONNECTING"
)

type ConnectionConfig struct {
	VenueCode       string
	MaxReconnect    int
	InitialBackoff  time.Duration
	MaxBackoff      time.Duration
	BackoffMultiplier float64
	PingInterval    time.Duration
	PongTimeout     time.Duration
}

func DefaultConnectionConfig() ConnectionConfig {
	return ConnectionConfig{
		MaxReconnect:      10,
		InitialBackoff:    1 * time.Second,
		MaxBackoff:        30 * time.Second,
		BackoffMultiplier: 2.0,
		PingInterval:      30 * time.Second,
		PongTimeout:       10 * time.Second,
	}
}

type Connection struct {
	ID            uuid.UUID
	VenueCode     string
	State         ConnectionState
	ConnectedAt   *time.Time
	DisconnectedAt *time.Time
	ReconnectCount int
	LastPingAt    *time.Time
	LastPongAt    *time.Time
	mu            sync.RWMutex
}

type ConnectionManager struct {
	connections map[uuid.UUID]*Connection
	venueConns  map[string]map[uuid.UUID]bool
	mu          sync.RWMutex
}

func NewConnectionManager() *ConnectionManager {
	return &ConnectionManager{
		connections: make(map[uuid.UUID]*Connection),
		venueConns:  make(map[string]map[uuid.UUID]bool),
	}
}

func (cm *ConnectionManager) CreateConnection(venueCode string) *Connection {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	conn := &Connection{
		ID:        uuid.New(),
		VenueCode: venueCode,
		State:     ConnectionStateDisconnected,
	}

	cm.connections[conn.ID] = conn

	if cm.venueConns[venueCode] == nil {
		cm.venueConns[venueCode] = make(map[uuid.UUID]bool)
	}
	cm.venueConns[venueCode][conn.ID] = true

	return conn
}

func (cm *ConnectionManager) GetConnection(id uuid.UUID) (*Connection, bool) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	conn, ok := cm.connections[id]
	return conn, ok
}

func (cm *ConnectionManager) GetVenueConnections(venueCode string) []*Connection {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	var conns []*Connection
	if ids, ok := cm.venueConns[venueCode]; ok {
		for id := range ids {
			if conn, ok := cm.connections[id]; ok {
				conns = append(conns, conn)
			}
		}
	}
	return conns
}

func (cm *ConnectionManager) UpdateState(id uuid.UUID, state ConnectionState) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if conn, ok := cm.connections[id]; ok {
		conn.mu.Lock()
		defer conn.mu.Unlock()

		now := time.Now()
		conn.State = state

		switch state {
		case ConnectionStateConnected:
			conn.ConnectedAt = &now
			conn.ReconnectCount = 0
		case ConnectionStateDisconnected:
			conn.DisconnectedAt = &now
		case ConnectionStateReconnecting:
			conn.ReconnectCount++
		}
	}
}

func (cm *ConnectionManager) UpdatePing(id uuid.UUID) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if conn, ok := cm.connections[id]; ok {
		conn.mu.Lock()
		defer conn.mu.Unlock()

		now := time.Now()
		conn.LastPingAt = &now
	}
}

func (cm *ConnectionManager) UpdatePong(id uuid.UUID) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if conn, ok := cm.connections[id]; ok {
		conn.mu.Lock()
		defer conn.mu.Unlock()

		now := time.Now()
		conn.LastPongAt = &now
	}
}

func (cm *ConnectionManager) RemoveConnection(id uuid.UUID) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if conn, ok := cm.connections[id]; ok {
		if ids, ok := cm.venueConns[conn.VenueCode]; ok {
			delete(ids, id)
		}
		delete(cm.connections, id)
	}
}

func (cm *ConnectionManager) GetState(id uuid.UUID) ConnectionState {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if conn, ok := cm.connections[id]; ok {
		conn.mu.RLock()
		defer conn.mu.RUnlock()
		return conn.State
	}
	return ConnectionStateDisconnected
}

func (cm *ConnectionManager) IsConnected(id uuid.UUID) bool {
	return cm.GetState(id) == ConnectionStateConnected
}

type BackoffCalculator struct {
	config ConnectionConfig
}

func NewBackoffCalculator(config ConnectionConfig) *BackoffCalculator {
	return &BackoffCalculator{config: config}
}

func (bc *BackoffCalculator) Calculate(reconnectCount int) time.Duration {
	backoff := bc.config.InitialBackoff

	for i := 0; i < reconnectCount; i++ {
		backoff = time.Duration(float64(backoff) * bc.config.BackoffMultiplier)
		if backoff > bc.config.MaxBackoff {
			return bc.config.MaxBackoff
		}
	}

	return backoff
}

type ReconnectHandler struct {
	manager    *ConnectionManager
	calculator *BackoffCalculator
	config     ConnectionConfig
}

func NewReconnectHandler(manager *ConnectionManager, config ConnectionConfig) *ReconnectHandler {
	return &ReconnectHandler{
		manager:    manager,
		calculator: NewBackoffCalculator(config),
		config:     config,
	}
}

func (rh *ReconnectHandler) HandleDisconnect(ctx context.Context, connID uuid.UUID, reconnect func(ctx context.Context) error) {
	go func() {
		for {
			rh.manager.UpdateState(connID, ConnectionStateReconnecting)

			backoff := rh.calculator.Calculate(rh.manager.GetReconnectCount(connID))

			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
				if err := reconnect(ctx); err != nil {
					continue
				}
				return
			}
		}
	}()
}

func (cm *ConnectionManager) GetReconnectCount(id uuid.UUID) int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if conn, ok := cm.connections[id]; ok {
		conn.mu.RLock()
		defer conn.mu.RUnlock()
		return conn.ReconnectCount
	}
	return 0
}
