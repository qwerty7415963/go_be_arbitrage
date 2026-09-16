package orderbook

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

func setupIntegrationRouter(handler *Handler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(gin.Recovery())

	v1 := router.Group("/api/v1")
	{
		orderbookRoutes := v1.Group("/orderbook")
		{
			orderbookRoutes.GET("/ws", handler.SubscribeWS)
			orderbookRoutes.GET("/depth", handler.GetOrderBook)
			orderbookRoutes.GET("/health", handler.GetHealth)
		}
	}

	return router
}

func newIntegrationService() *Service {
	repo := &Repository{}
	return NewService(repo)
}

func TestIntegrationWS_ConnectReceiveInitialSnapshot(t *testing.T) {
	service := newIntegrationService()
	hub := NewWsHub()
	go hub.Run()
	handler := NewHandler(service, hub)
	router := setupIntegrationRouter(handler)

	venueID := uuid.New()
	instrumentID := uuid.New()

	// Apply snapshot BEFORE client connects
	snapshot := &OrderBookSnapshot{
		VenueID:      venueID,
		InstrumentID: instrumentID,
		Sequence:     1,
		BestBid:      strPtr("50000.00"),
		BestAsk:      strPtr("50001.00"),
		Bids: []PriceLevel{
			{Price: "50000.00", Quantity: "1.5"},
			{Price: "49999.00", Quantity: "2.0"},
		},
		Asks: []PriceLevel{
			{Price: "50001.00", Quantity: "0.8"},
			{Price: "50002.00", Quantity: "1.2"},
		},
		Timestamp: time.Now(),
	}
	book := service.engine.GetOrCreateBook(venueID, instrumentID, 5*time.Second, 10*time.Second)
	service.engine.ApplySnapshot(book, snapshot)

	ts := httptest.NewServer(router)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/orderbook/ws?venue_id=" + venueID.String() + "&instrument_id=" + instrumentID.String()

	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect ws: %v", err)
	}
	defer ws.Close()

	ws.SetReadDeadline(time.Now().Add(3 * time.Second))

	_, msg, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read initial snapshot: %v", err)
	}

	var wsMsg WsMessage
	if err := json.Unmarshal(msg, &wsMsg); err != nil {
		t.Fatalf("failed to unmarshal message: %v", err)
	}

	if wsMsg.Type != "snapshot" {
		t.Errorf("expected type 'snapshot', got '%s'", wsMsg.Type)
	}

	data, ok := wsMsg.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected data to be map, got %T", wsMsg.Data)
	}

	if data["sequence"].(float64) != 1 {
		t.Errorf("expected sequence 1, got %v", data["sequence"])
	}

	bestBid := data["best_bid"].(string)
	if bestBid != "50000.00" {
		t.Errorf("expected best_bid 50000.00, got %s", bestBid)
	}

	bestAsk := data["best_ask"].(string)
	if bestAsk != "50001.00" {
		t.Errorf("expected best_ask 50001.00, got %s", bestAsk)
	}

	bids := data["bids"].([]interface{})
	if len(bids) != 2 {
		t.Errorf("expected 2 bids, got %d", len(bids))
	}

	asks := data["asks"].([]interface{})
	if len(asks) != 2 {
		t.Errorf("expected 2 asks, got %d", len(asks))
	}
}

func TestIntegrationWS_RealtimeUpdatePushed(t *testing.T) {
	service := newIntegrationService()
	hub := NewWsHub()
	go hub.Run()
	handler := NewHandler(service, hub)
	router := setupIntegrationRouter(handler)

	venueID := uuid.New()
	instrumentID := uuid.New()

	// Apply initial snapshot
	snapshot1 := &OrderBookSnapshot{
		VenueID:      venueID,
		InstrumentID: instrumentID,
		Sequence:     1,
		BestBid:      strPtr("100.00"),
		BestAsk:      strPtr("101.00"),
		Bids:         []PriceLevel{{Price: "100.00", Quantity: "10"}},
		Asks:         []PriceLevel{{Price: "101.00", Quantity: "5"}},
		Timestamp:    time.Now(),
	}
	book := service.engine.GetOrCreateBook(venueID, instrumentID, 5*time.Second, 10*time.Second)
	service.engine.ApplySnapshot(book, snapshot1)

	ts := httptest.NewServer(router)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/orderbook/ws?venue_id=" + venueID.String() + "&instrument_id=" + instrumentID.String()

	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect ws: %v", err)
	}
	defer ws.Close()

	ws.SetReadDeadline(time.Now().Add(5 * time.Second))

	// Read initial snapshot
	_, msg, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read initial snapshot: %v", err)
	}

	var wsMsg WsMessage
	json.Unmarshal(msg, &wsMsg)
	data := wsMsg.Data.(map[string]interface{})
	if data["sequence"].(float64) != 1 {
		t.Fatalf("expected initial sequence 1, got %v", data["sequence"])
	}

	// Wait for subscription goroutine to fully start
	time.Sleep(200 * time.Millisecond)

	// Apply new snapshot and notify subscribers
	snapshot2 := &OrderBookSnapshot{
		VenueID:      venueID,
		InstrumentID: instrumentID,
		Sequence:     2,
		BestBid:      strPtr("200.00"),
		BestAsk:      strPtr("201.00"),
		Bids:         []PriceLevel{{Price: "200.00", Quantity: "20"}},
		Asks:         []PriceLevel{{Price: "201.00", Quantity: "15"}},
		Timestamp:    time.Now(),
	}
	service.engine.ApplySnapshot(book, snapshot2)
	service.NotifySubscribers(snapshot2)

	// Read second snapshot
	_, msg, err = ws.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read second snapshot: %v", err)
	}

	var wsMsg2 WsMessage
	json.Unmarshal(msg, &wsMsg2)

	if wsMsg2.Type != "snapshot" {
		t.Errorf("expected type 'snapshot', got '%s'", wsMsg2.Type)
	}

	data2 := wsMsg2.Data.(map[string]interface{})
	if data2["sequence"].(float64) != 2 {
		t.Errorf("expected sequence 2, got %v", data2["sequence"])
	}

	bestBid := data2["best_bid"].(string)
	if bestBid != "200.00" {
		t.Errorf("expected best_bid 200.00, got %s", bestBid)
	}
}

func TestIntegrationWS_MultipleClients(t *testing.T) {
	service := newIntegrationService()
	hub := NewWsHub()
	go hub.Run()
	handler := NewHandler(service, hub)
	router := setupIntegrationRouter(handler)

	venueID := uuid.New()
	instrumentID := uuid.New()

	snapshot := &OrderBookSnapshot{
		VenueID:      venueID,
		InstrumentID: instrumentID,
		Sequence:     1,
		Bids:         []PriceLevel{{Price: "100", Quantity: "10"}},
		Asks:         []PriceLevel{{Price: "101", Quantity: "5"}},
		Timestamp:    time.Now(),
	}
	book := service.engine.GetOrCreateBook(venueID, instrumentID, 5*time.Second, 10*time.Second)
	service.engine.ApplySnapshot(book, snapshot)

	ts := httptest.NewServer(router)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/orderbook/ws?venue_id=" + venueID.String() + "&instrument_id=" + instrumentID.String()

	var wg sync.WaitGroup
	var mu sync.Mutex
	var messages []*WsMessage

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(clientID int) {
			defer wg.Done()

			ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
			if err != nil {
				t.Errorf("client %d: failed to connect: %v", clientID, err)
				return
			}
			defer ws.Close()

			ws.SetReadDeadline(time.Now().Add(3 * time.Second))

			_, msg, err := ws.ReadMessage()
			if err != nil {
				t.Errorf("client %d: failed to read: %v", clientID, err)
				return
			}

			var wsMsg WsMessage
			json.Unmarshal(msg, &wsMsg)

			mu.Lock()
			messages = append(messages, &wsMsg)
			mu.Unlock()
		}(i)
	}

	wg.Wait()

	if len(messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(messages))
	}

	for i, msg := range messages {
		if msg.Type != "snapshot" {
			t.Errorf("client %d: expected type 'snapshot', got '%s'", i, msg.Type)
		}
	}
}

func TestIntegrationWS_InvalidVenueID(t *testing.T) {
	service := newIntegrationService()
	hub := NewWsHub()
	go hub.Run()
	handler := NewHandler(service, hub)
	router := setupIntegrationRouter(handler)

	ts := httptest.NewServer(router)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/orderbook/ws?venue_id=invalid&instrument_id=" + uuid.New().String()

	ws, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err == nil {
		ws.Close()
		t.Fatal("expected error connecting ws")
	}
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestIntegrationWS_InvalidInstrumentID(t *testing.T) {
	service := newIntegrationService()
	hub := NewWsHub()
	go hub.Run()
	handler := NewHandler(service, hub)
	router := setupIntegrationRouter(handler)

	ts := httptest.NewServer(router)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/orderbook/ws?venue_id=" + uuid.New().String() + "&instrument_id=invalid"

	ws, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err == nil {
		ws.Close()
		t.Fatal("expected error connecting ws")
	}
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestIntegrationWS_MissingParams(t *testing.T) {
	service := newIntegrationService()
	hub := NewWsHub()
	go hub.Run()
	handler := NewHandler(service, hub)
	router := setupIntegrationRouter(handler)

	ts := httptest.NewServer(router)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/orderbook/ws"

	ws, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err == nil {
		ws.Close()
		t.Fatal("expected error connecting ws")
	}
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestIntegrationWS_ClientDisconnect(t *testing.T) {
	service := newIntegrationService()
	hub := NewWsHub()
	go hub.Run()
	handler := NewHandler(service, hub)
	router := setupIntegrationRouter(handler)

	venueID := uuid.New()
	instrumentID := uuid.New()

	snapshot := &OrderBookSnapshot{
		VenueID:      venueID,
		InstrumentID: instrumentID,
		Sequence:     1,
		Bids:         []PriceLevel{{Price: "100", Quantity: "10"}},
		Asks:         []PriceLevel{{Price: "101", Quantity: "5"}},
		Timestamp:    time.Now(),
	}
	book := service.engine.GetOrCreateBook(venueID, instrumentID, 5*time.Second, 10*time.Second)
	service.engine.ApplySnapshot(book, snapshot)

	ts := httptest.NewServer(router)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/orderbook/ws?venue_id=" + venueID.String() + "&instrument_id=" + instrumentID.String()

	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect ws: %v", err)
	}

	ws.SetReadDeadline(time.Now().Add(2 * time.Second))

	_, _, err = ws.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read initial snapshot: %v", err)
	}

	// Disconnect client
	ws.Close()
	time.Sleep(100 * time.Millisecond)

	// Hub should have removed the client
	if hub.ClientCount() != 0 {
		t.Errorf("expected 0 clients after disconnect, got %d", hub.ClientCount())
	}
}

func TestIntegrationWS_SnapshotContainsAllFields(t *testing.T) {
	service := newIntegrationService()
	hub := NewWsHub()
	go hub.Run()
	handler := NewHandler(service, hub)
	router := setupIntegrationRouter(handler)

	venueID := uuid.New()
	instrumentID := uuid.New()

	now := time.Now().UTC().Truncate(time.Millisecond)
	snapshot := &OrderBookSnapshot{
		VenueID:      venueID,
		InstrumentID: instrumentID,
		Sequence:     42,
		BestBid:      strPtr("65432.10"),
		BestAsk:      strPtr("65433.20"),
		Bids: []PriceLevel{
			{Price: "65432.10", Quantity: "0.5"},
			{Price: "65431.00", Quantity: "1.2"},
			{Price: "65430.00", Quantity: "3.0"},
		},
		Asks: []PriceLevel{
			{Price: "65433.20", Quantity: "0.3"},
			{Price: "65434.00", Quantity: "2.1"},
			{Price: "65435.00", Quantity: "5.0"},
		},
		Timestamp: now,
	}
	book := service.engine.GetOrCreateBook(venueID, instrumentID, 5*time.Second, 10*time.Second)
	service.engine.ApplySnapshot(book, snapshot)

	ts := httptest.NewServer(router)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/orderbook/ws?venue_id=" + venueID.String() + "&instrument_id=" + instrumentID.String()

	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect ws: %v", err)
	}
	defer ws.Close()

	ws.SetReadDeadline(time.Now().Add(3 * time.Second))

	_, msg, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read snapshot: %v", err)
	}

	var wsMsg WsMessage
	json.Unmarshal(msg, &wsMsg)

	data := wsMsg.Data.(map[string]interface{})

	if data["venue_id"] != venueID.String() {
		t.Errorf("expected venue_id %s, got %v", venueID, data["venue_id"])
	}
	if data["instrument_id"] != instrumentID.String() {
		t.Errorf("expected instrument_id %s, got %v", instrumentID, data["instrument_id"])
	}
	if data["sequence"].(float64) != 42 {
		t.Errorf("expected sequence 42, got %v", data["sequence"])
	}
	if data["best_bid"] != "65432.10" {
		t.Errorf("expected best_bid 65432.10, got %v", data["best_bid"])
	}
	if data["best_ask"] != "65433.20" {
		t.Errorf("expected best_ask 65433.20, got %v", data["best_ask"])
	}

	bids := data["bids"].([]interface{})
	if len(bids) != 3 {
		t.Fatalf("expected 3 bids, got %d", len(bids))
	}

	firstBid := bids[0].(map[string]interface{})
	if firstBid["price"] != "65432.10" {
		t.Errorf("expected first bid price 65432.10, got %v", firstBid["price"])
	}
	if firstBid["quantity"] != "0.5" {
		t.Errorf("expected first bid quantity 0.5, got %v", firstBid["quantity"])
	}

	asks := data["asks"].([]interface{})
	if len(asks) != 3 {
		t.Fatalf("expected 3 asks, got %d", len(asks))
	}

	firstAsk := asks[0].(map[string]interface{})
	if firstAsk["price"] != "65433.20" {
		t.Errorf("expected first ask price 65433.20, got %v", firstAsk["price"])
	}
}
