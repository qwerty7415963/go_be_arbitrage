package orderbook

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/exchange"
)

// WSAdapter defines the interface for WebSocket adapters that provide orderbook events
type WSAdapter interface {
	SubscribeOrderBook(ctx context.Context, symbol string, depth int) (<-chan *exchange.OrderBookEvent, error)
	Disconnect(ctx context.Context) error
	IsConnected() bool
}

// BridgeConfig holds configuration for the orderbook bridge
type BridgeConfig struct {
	StaleThreshold time.Duration
	SyncThreshold  time.Duration
	ReconnectDelay time.Duration
}

// DefaultBridgeConfig returns sensible defaults
func DefaultBridgeConfig() BridgeConfig {
	return BridgeConfig{
		StaleThreshold: 30 * time.Second,
		SyncThreshold:  10 * time.Second,
		ReconnectDelay: 5 * time.Second,
	}
}

// Bridge connects WS adapters to the orderbook engine
type Bridge struct {
	engine   *Engine
	config   BridgeConfig
	registry *InstrumentRegistry
	handlers map[string]*symbolHandler
	mu       sync.Mutex
}

// InstrumentRegistry maps venue symbols to (venueID, instrumentID)
type InstrumentRegistry struct {
	entries map[string]registryEntry
	mu      sync.RWMutex
}

type registryEntry struct {
	VenueID      uuid.UUID
	InstrumentID uuid.UUID
}

// NewInstrumentRegistry creates a new registry
func NewInstrumentRegistry() *InstrumentRegistry {
	return &InstrumentRegistry{
		entries: make(map[string]registryEntry),
	}
}

// Register maps a venue symbol to venue+instrument IDs
func (r *InstrumentRegistry) Register(venueSymbol string, venueID, instrumentID uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries[venueSymbol] = registryEntry{VenueID: venueID, InstrumentID: instrumentID}
}

// Lookup returns venue+instrument IDs for a symbol
func (r *InstrumentRegistry) Lookup(venueSymbol string) (uuid.UUID, uuid.UUID, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entry, ok := r.entries[venueSymbol]
	return entry.VenueID, entry.InstrumentID, ok
}

// symbolHandler manages a single symbol's WS feed and orderbook
type symbolHandler struct {
	symbol    string
	venueID   uuid.UUID
	instID    uuid.UUID
	adapter   WSAdapter
	engine    *Engine
	config    BridgeConfig
	cancel    context.CancelFunc
	gapCount  int
	lastSeq   int64
	mu        sync.Mutex
}

// NewBridge creates a new orderbook bridge
func NewBridge(engine *Engine, registry *InstrumentRegistry, config BridgeConfig) *Bridge {
	return &Bridge{
		engine:   engine,
		config:   config,
		registry: registry,
		handlers: make(map[string]*symbolHandler),
	}
}

// Subscribe starts feeding orderbook events for a symbol into the engine
func (b *Bridge) Subscribe(ctx context.Context, adapter WSAdapter, venueSymbol string, depth int) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, exists := b.handlers[venueSymbol]; exists {
		return nil
	}

	venueID, instID, ok := b.registry.Lookup(venueSymbol)
	if !ok {
		return ErrInstrumentNotFound
	}

	eventChan, err := adapter.SubscribeOrderBook(ctx, venueSymbol, depth)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(ctx)

	handler := &symbolHandler{
		symbol:  venueSymbol,
		venueID: venueID,
		instID:  instID,
		adapter: adapter,
		engine:  b.engine,
		config:  b.config,
		cancel:  cancel,
	}

	b.handlers[venueSymbol] = handler

	go handler.consumeEvents(ctx, eventChan)

	return nil
}

// Unsubscribe stops feeding orderbook events for a symbol
func (b *Bridge) Unsubscribe(venueSymbol string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if handler, ok := b.handlers[venueSymbol]; ok {
		handler.cancel()
		delete(b.handlers, venueSymbol)
	}
}

// GetHealth returns the health of the orderbook for a symbol
func (b *Bridge) GetHealth(venueSymbol string) *OrderBookHealth {
	venueID, instID, ok := b.registry.Lookup(venueSymbol)
	if !ok {
		return nil
	}

	book := b.engine.GetBook(venueID, instID)
	if book == nil {
		return nil
	}

	return b.engine.GetHealth(book)
}

// IsHealthy returns true if the orderbook for a symbol is healthy
func (b *Bridge) IsHealthy(venueSymbol string) bool {
	health := b.GetHealth(venueSymbol)
	if health == nil {
		return false
	}
	return health.IsHealthy
}

// GetDepth returns the orderbook depth for a symbol
func (b *Bridge) GetDepth(venueSymbol string, depth int) *OrderBookDepth {
	venueID, instID, ok := b.registry.Lookup(venueSymbol)
	if !ok {
		return nil
	}

	book := b.engine.GetBook(venueID, instID)
	if book == nil {
		return nil
	}

	return b.engine.GetDepth(book, depth)
}

// consumeEvents processes WS events and applies them to the engine
func (h *symbolHandler) consumeEvents(ctx context.Context, eventChan <-chan *exchange.OrderBookEvent) {
	book := h.engine.GetOrCreateBook(h.venueID, h.instID, h.config.StaleThreshold, h.config.SyncThreshold)
	var gotSnapshot bool

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-eventChan:
			if !ok {
				h.handleDisconnect()
				return
			}
			h.processEvent(book, event, &gotSnapshot)
		}
	}
}

// processEvent applies a single event to the orderbook
func (h *symbolHandler) processEvent(book *OrderBook, event *exchange.OrderBookEvent, gotSnapshot *bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if event.IsSnapshot {
		snapshot := h.convertSnapshot(event)
		if err := h.engine.ApplySnapshot(book, snapshot); err != nil {
			log.Printf("bridge: apply snapshot error for %s: %v", h.symbol, err)
			return
		}
		h.lastSeq = event.Sequence
		*gotSnapshot = true
		h.gapCount = 0
		return
	}

	if !*gotSnapshot {
		return
	}

	delta := h.convertDelta(event)
	if err := h.engine.ApplyDelta(book, delta); err != nil {
		h.gapCount++
		log.Printf("bridge: delta error for %s (gap=%d): %v", h.symbol, h.gapCount, err)

		if h.gapCount >= 3 {
			h.requestResync(book)
		}
		return
	}

	h.lastSeq = event.Sequence
	h.gapCount = 0
}

// handleDisconnect marks the book as disconnected
func (h *symbolHandler) handleDisconnect() {
	h.mu.Lock()
	defer h.mu.Unlock()

	book := h.engine.GetBook(h.venueID, h.instID)
	if book != nil {
		book.mu.Lock()
		book.State = BookStateDisconnected
		book.mu.Unlock()
	}
}

// requestResync triggers a resync
func (h *symbolHandler) requestResync(book *OrderBook) {
	h.engine.RequestResync(book)
	log.Printf("bridge: resync requested for %s (resync_count=%d)", h.symbol, book.ResyncCount)
}

// convertSnapshot converts an exchange event to an orderbook snapshot
func (h *symbolHandler) convertSnapshot(event *exchange.OrderBookEvent) *OrderBookSnapshot {
	bids := make([]PriceLevel, len(event.Bids))
	for i, b := range event.Bids {
		bids[i] = PriceLevel{Price: b.Price, Quantity: b.Quantity}
	}

	asks := make([]PriceLevel, len(event.Asks))
	for i, a := range event.Asks {
		asks[i] = PriceLevel{Price: a.Price, Quantity: a.Quantity}
	}

	var bestBid, bestAsk *string
	if len(bids) > 0 {
		bestBid = &bids[0].Price
	}
	if len(asks) > 0 {
		bestAsk = &asks[0].Price
	}

	return &OrderBookSnapshot{
		VenueID:      h.venueID,
		InstrumentID: h.instID,
		Sequence:     event.Sequence,
		BestBid:      bestBid,
		BestAsk:      bestAsk,
		Bids:         bids,
		Asks:         asks,
		Timestamp:    event.Timestamp,
	}
}

// convertDelta converts an exchange event to an orderbook delta
func (h *symbolHandler) convertDelta(event *exchange.OrderBookEvent) *OrderBookDelta {
	bids := make([]PriceLevel, len(event.Bids))
	for i, b := range event.Bids {
		bids[i] = PriceLevel{Price: b.Price, Quantity: b.Quantity}
	}

	asks := make([]PriceLevel, len(event.Asks))
	for i, a := range event.Asks {
		asks[i] = PriceLevel{Price: a.Price, Quantity: a.Quantity}
	}

	fromSeq := event.FromSequence
	if fromSeq == 0 {
		fromSeq = h.lastSeq
	}

	return &OrderBookDelta{
		VenueID:      h.venueID,
		InstrumentID: h.instID,
		FromSequence: fromSeq,
		ToSequence:   event.Sequence,
		Bids:         bids,
		Asks:         asks,
		Timestamp:    event.Timestamp,
	}
}
