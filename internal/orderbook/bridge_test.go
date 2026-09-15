package orderbook

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/exchange"
)

func TestEngine_ApplyDelta_DuplicateIgnored(t *testing.T) {
	engine := NewEngine()
	venueID := uuid.New()
	instrumentID := uuid.New()

	book := engine.GetOrCreateBook(venueID, instrumentID, 5*time.Second, 10*time.Second)

	snapshot := &OrderBookSnapshot{
		VenueID:      venueID,
		InstrumentID: instrumentID,
		Sequence:     1,
		Bids:         []PriceLevel{{Price: "100", Quantity: "10"}},
		Asks:         []PriceLevel{{Price: "101", Quantity: "5"}},
		Timestamp:    time.Now(),
	}
	engine.ApplySnapshot(book, snapshot)

	delta := &OrderBookDelta{
		VenueID:      venueID,
		InstrumentID: instrumentID,
		FromSequence: 1,
		ToSequence:   2,
		Bids:         []PriceLevel{{Price: "100", Quantity: "15"}},
		Timestamp:    time.Now(),
	}
	engine.ApplyDelta(book, delta)

	if book.Sequence != 2 {
		t.Fatalf("expected sequence 2, got %d", book.Sequence)
	}

	duplicate := &OrderBookDelta{
		VenueID:      venueID,
		InstrumentID: instrumentID,
		FromSequence: 1,
		ToSequence:   2,
		Bids:         []PriceLevel{{Price: "100", Quantity: "99"}},
		Timestamp:    time.Now(),
	}
	err := engine.ApplyDelta(book, duplicate)
	if err != nil {
		t.Errorf("expected nil error for duplicate, got %v", err)
	}
	if book.Sequence != 2 {
		t.Errorf("expected sequence unchanged at 2, got %d", book.Sequence)
	}
}

func TestEngine_ApplyDelta_StaleDeltaIgnored(t *testing.T) {
	engine := NewEngine()
	venueID := uuid.New()
	instrumentID := uuid.New()

	book := engine.GetOrCreateBook(venueID, instrumentID, 5*time.Second, 10*time.Second)

	snapshot := &OrderBookSnapshot{
		VenueID:      venueID,
		InstrumentID: instrumentID,
		Sequence:     5,
		Bids:         []PriceLevel{{Price: "100", Quantity: "10"}},
		Asks:         []PriceLevel{{Price: "101", Quantity: "5"}},
		Timestamp:    time.Now(),
	}
	engine.ApplySnapshot(book, snapshot)

	stale := &OrderBookDelta{
		VenueID:      venueID,
		InstrumentID: instrumentID,
		FromSequence: 3,
		ToSequence:   4,
		Bids:         []PriceLevel{{Price: "100", Quantity: "99"}},
		Timestamp:    time.Now(),
	}
	err := engine.ApplyDelta(book, stale)
	if err != nil {
		t.Errorf("expected nil error for stale delta, got %v", err)
	}
	if book.Sequence != 5 {
		t.Errorf("expected sequence unchanged at 5, got %d", book.Sequence)
	}
}

func TestEngine_ApplySnapshot_Reconnect(t *testing.T) {
	engine := NewEngine()
	venueID := uuid.New()
	instrumentID := uuid.New()

	book := engine.GetOrCreateBook(venueID, instrumentID, 5*time.Second, 10*time.Second)

	snapshot := &OrderBookSnapshot{
		VenueID:      venueID,
		InstrumentID: instrumentID,
		Sequence:     1,
		Bids:         []PriceLevel{{Price: "100", Quantity: "10"}},
		Asks:         []PriceLevel{{Price: "101", Quantity: "5"}},
		Timestamp:    time.Now(),
	}
	engine.ApplySnapshot(book, snapshot)

	book.State = BookStateResyncing
	book.GapCount = 5

	newSnapshot := &OrderBookSnapshot{
		VenueID:      venueID,
		InstrumentID: instrumentID,
		Sequence:     100,
		Bids:         []PriceLevel{{Price: "200", Quantity: "20"}},
		Asks:         []PriceLevel{{Price: "201", Quantity: "10"}},
		Timestamp:    time.Now(),
	}
	engine.ApplySnapshot(book, newSnapshot)

	if book.State != BookStateHealthy {
		t.Errorf("expected HEALTHY after reconnect, got %s", book.State)
	}
	if book.GapCount != 0 {
		t.Errorf("expected gap count reset to 0, got %d", book.GapCount)
	}
	if book.Sequence != 100 {
		t.Errorf("expected sequence 100, got %d", book.Sequence)
	}
}

// mockAdapter is a mock WS adapter for testing
type mockAdapter struct {
	eventChan chan *exchange.OrderBookEvent
	connected bool
	mu        sync.Mutex
}

func newMockAdapter() *mockAdapter {
	return &mockAdapter{
		eventChan: make(chan *exchange.OrderBookEvent, 100),
		connected: true,
	}
}

func (m *mockAdapter) SubscribeOrderBook(ctx context.Context, symbol string, depth int) (<-chan *exchange.OrderBookEvent, error) {
	return m.eventChan, nil
}

func (m *mockAdapter) Disconnect(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connected = false
	return nil
}

func (m *mockAdapter) IsConnected() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.connected
}

func (m *mockAdapter) sendEvent(event *exchange.OrderBookEvent) {
	m.eventChan <- event
}

func TestBridge_SnapshotThenDeltas(t *testing.T) {
	engine := NewEngine()
	registry := NewInstrumentRegistry()
	venueID := uuid.New()
	instID := uuid.New()
	registry.Register("BTCUSDT", venueID, instID)

	config := DefaultBridgeConfig()
	config.StaleThreshold = 5 * time.Second
	config.SyncThreshold = 10 * time.Second

	bridge := NewBridge(engine, registry, config)
	adapter := newMockAdapter()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := bridge.Subscribe(ctx, adapter, "BTCUSDT", 20)
	if err != nil {
		t.Fatalf("subscribe error: %v", err)
	}

	adapter.sendEvent(&exchange.OrderBookEvent{
		VenueSymbol: "BTCUSDT",
		Bids:        []exchange.PriceLevel{{Price: "100", Quantity: "10"}},
		Asks:        []exchange.PriceLevel{{Price: "101", Quantity: "5"}},
		Timestamp:   time.Now(),
		IsSnapshot:  true,
		Sequence:    1,
	})

	time.Sleep(50 * time.Millisecond)

	book := engine.GetBook(venueID, instID)
	if book == nil {
		t.Fatal("expected book to exist")
	}
	if book.Sequence != 1 {
		t.Errorf("expected sequence 1, got %d", book.Sequence)
	}
	if len(book.Bids) != 1 {
		t.Errorf("expected 1 bid, got %d", len(book.Bids))
	}

	adapter.sendEvent(&exchange.OrderBookEvent{
		VenueSymbol: "BTCUSDT",
		Bids:        []exchange.PriceLevel{{Price: "100", Quantity: "15"}},
		Timestamp:   time.Now(),
		Sequence:    2,
	})

	time.Sleep(50 * time.Millisecond)

	if book.Sequence != 2 {
		t.Errorf("expected sequence 2, got %d", book.Sequence)
	}
	if book.Bids[0].Quantity != "15" {
		t.Errorf("expected quantity 15, got %s", book.Bids[0].Quantity)
	}
}

func TestBridge_DuplicateDeltaIgnored(t *testing.T) {
	engine := NewEngine()
	registry := NewInstrumentRegistry()
	venueID := uuid.New()
	instID := uuid.New()
	registry.Register("ETHUSDT", venueID, instID)

	config := DefaultBridgeConfig()
	bridge := NewBridge(engine, registry, config)
	adapter := newMockAdapter()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bridge.Subscribe(ctx, adapter, "ETHUSDT", 20)

	adapter.sendEvent(&exchange.OrderBookEvent{
		VenueSymbol: "ETHUSDT",
		Bids:        []exchange.PriceLevel{{Price: "2000", Quantity: "1"}},
		Asks:        []exchange.PriceLevel{{Price: "2001", Quantity: "2"}},
		Timestamp:   time.Now(),
		IsSnapshot:  true,
		Sequence:    10,
	})
	time.Sleep(50 * time.Millisecond)

	adapter.sendEvent(&exchange.OrderBookEvent{
		VenueSymbol: "ETHUSDT",
		Bids:        []exchange.PriceLevel{{Price: "2000", Quantity: "5"}},
		Timestamp:   time.Now(),
		Sequence:    12,
	})
	time.Sleep(50 * time.Millisecond)

	book := engine.GetBook(venueID, instID)
	if book.Sequence != 12 {
		t.Fatalf("expected sequence 12, got %d", book.Sequence)
	}

	adapter.sendEvent(&exchange.OrderBookEvent{
		VenueSymbol: "ETHUSDT",
		Bids:        []exchange.PriceLevel{{Price: "2000", Quantity: "99"}},
		Timestamp:   time.Now(),
		Sequence:    11,
	})
	time.Sleep(50 * time.Millisecond)

	if book.Sequence != 12 {
		t.Errorf("expected sequence unchanged at 12, got %d", book.Sequence)
	}
}

func TestBridge_GapTriggersResync(t *testing.T) {
	engine := NewEngine()
	registry := NewInstrumentRegistry()
	venueID := uuid.New()
	instID := uuid.New()
	registry.Register("SOLUSDT", venueID, instID)

	config := DefaultBridgeConfig()
	bridge := NewBridge(engine, registry, config)
	adapter := newMockAdapter()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bridge.Subscribe(ctx, adapter, "SOLUSDT", 20)

	adapter.sendEvent(&exchange.OrderBookEvent{
		VenueSymbol: "SOLUSDT",
		Bids:        []exchange.PriceLevel{{Price: "100", Quantity: "10"}},
		Asks:        []exchange.PriceLevel{{Price: "101", Quantity: "5"}},
		Timestamp:   time.Now(),
		IsSnapshot:  true,
		Sequence:    1,
	})
	time.Sleep(50 * time.Millisecond)

	for i := 0; i < 3; i++ {
		adapter.sendEvent(&exchange.OrderBookEvent{
			VenueSymbol:  "SOLUSDT",
			Bids:         []exchange.PriceLevel{{Price: "100", Quantity: "10"}},
			Timestamp:    time.Now(),
			FromSequence: int64(50 + i*100),
			Sequence:     int64(100 + i*100),
		})
		time.Sleep(50 * time.Millisecond)
	}

	book := engine.GetBook(venueID, instID)
	if book.State != BookStateResyncing {
		t.Errorf("expected RESYNCING after 3 gaps, got %s", book.State)
	}
	if book.ResyncCount != 1 {
		t.Errorf("expected resync count 1, got %d", book.ResyncCount)
	}
}

func TestBridge_DisconnectMarksBook(t *testing.T) {
	engine := NewEngine()
	registry := NewInstrumentRegistry()
	venueID := uuid.New()
	instID := uuid.New()
	registry.Register("DOGEUSDT", venueID, instID)

	config := DefaultBridgeConfig()
	bridge := NewBridge(engine, registry, config)
	adapter := newMockAdapter()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bridge.Subscribe(ctx, adapter, "DOGEUSDT", 20)

	adapter.sendEvent(&exchange.OrderBookEvent{
		VenueSymbol: "DOGEUSDT",
		Bids:        []exchange.PriceLevel{{Price: "0.1", Quantity: "1000"}},
		Asks:        []exchange.PriceLevel{{Price: "0.11", Quantity: "2000"}},
		Timestamp:   time.Now(),
		IsSnapshot:  true,
		Sequence:    1,
	})
	time.Sleep(50 * time.Millisecond)

	close(adapter.eventChan)
	time.Sleep(100 * time.Millisecond)

	book := engine.GetBook(venueID, instID)
	if book.State != BookStateDisconnected {
		t.Errorf("expected DISCONNECTED, got %s", book.State)
	}
}

func TestBridge_HealthAndDepth(t *testing.T) {
	engine := NewEngine()
	registry := NewInstrumentRegistry()
	venueID := uuid.New()
	instID := uuid.New()
	registry.Register("BTCUSDT", venueID, instID)

	config := DefaultBridgeConfig()
	bridge := NewBridge(engine, registry, config)
	adapter := newMockAdapter()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bridge.Subscribe(ctx, adapter, "BTCUSDT", 20)

	adapter.sendEvent(&exchange.OrderBookEvent{
		VenueSymbol: "BTCUSDT",
		Bids: []exchange.PriceLevel{
			{Price: "100", Quantity: "10"},
			{Price: "99", Quantity: "20"},
		},
		Asks: []exchange.PriceLevel{
			{Price: "101", Quantity: "5"},
			{Price: "102", Quantity: "15"},
		},
		Timestamp:  time.Now(),
		IsSnapshot: true,
		Sequence:   1,
	})
	time.Sleep(50 * time.Millisecond)

	health := bridge.GetHealth("BTCUSDT")
	if health == nil {
		t.Fatal("expected health")
	}
	if !health.IsHealthy {
		t.Error("expected healthy")
	}

	depth := bridge.GetDepth("BTCUSDT", 2)
	if depth == nil {
		t.Fatal("expected depth")
	}
	if depth.BestBid == nil || *depth.BestBid != "100" {
		t.Errorf("expected best bid 100, got %v", depth.BestBid)
	}
	if depth.BestAsk == nil || *depth.BestAsk != "101" {
		t.Errorf("expected best ask 101, got %v", depth.BestAsk)
	}
	if len(depth.BidDepth) != 2 {
		t.Errorf("expected 2 bid depth, got %d", len(depth.BidDepth))
	}
}

func TestInstrumentRegistry_LookupNotFound(t *testing.T) {
	registry := NewInstrumentRegistry()

	_, _, ok := registry.Lookup("NONEXISTENT")
	if ok {
		t.Error("expected not found")
	}
}

func TestInstrumentRegistry_RegisterAndLookup(t *testing.T) {
	registry := NewInstrumentRegistry()
	venueID := uuid.New()
	instID := uuid.New()

	registry.Register("BTCUSDT", venueID, instID)

	gotVenue, gotInst, ok := registry.Lookup("BTCUSDT")
	if !ok {
		t.Fatal("expected found")
	}
	if gotVenue != venueID {
		t.Errorf("expected venue %s, got %s", venueID, gotVenue)
	}
	if gotInst != instID {
		t.Errorf("expected instrument %s, got %s", instID, gotInst)
	}
}
