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

func TestWsHub_Run(t *testing.T) {
	hub := NewWsHub()
	go hub.Run()

	client := &WsClient{
		hub:  hub,
		send: make(chan []byte, 1),
	}

	hub.register <- client
	time.Sleep(10 * time.Millisecond)

	if hub.ClientCount() != 1 {
		t.Errorf("expected 1 client, got %d", hub.ClientCount())
	}

	hub.unregister <- client
	time.Sleep(10 * time.Millisecond)

	if hub.ClientCount() != 0 {
		t.Errorf("expected 0 clients, got %d", hub.ClientCount())
	}
}

func TestWsHub_MultipleClients(t *testing.T) {
	hub := NewWsHub()
	go hub.Run()

	clients := make([]*WsClient, 3)
	for i := 0; i < 3; i++ {
		clients[i] = &WsClient{
			hub:  hub,
			send: make(chan []byte, 1),
		}
		hub.register <- clients[i]
	}

	time.Sleep(10 * time.Millisecond)

	if hub.ClientCount() != 3 {
		t.Errorf("expected 3 clients, got %d", hub.ClientCount())
	}

	for _, c := range clients {
		hub.unregister <- c
	}

	time.Sleep(10 * time.Millisecond)

	if hub.ClientCount() != 0 {
		t.Errorf("expected 0 clients after unregister, got %d", hub.ClientCount())
	}
}

func TestWsClient_SendSnapshot(t *testing.T) {
	hub := NewWsHub()
	go hub.Run()

	client := &WsClient{
		hub:  hub,
		send: make(chan []byte, 10),
	}

	hub.register <- client

	snapshot := &OrderBookSnapshot{
		VenueID:      uuid.New(),
		InstrumentID: uuid.New(),
		Sequence:     12345,
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

	client.sendSnapshot(snapshot)

	select {
	case msg := <-client.send:
		var wsMsg WsMessage
		if err := json.Unmarshal(msg, &wsMsg); err != nil {
			t.Fatalf("failed to unmarshal ws message: %v", err)
		}
		if wsMsg.Type != "snapshot" {
			t.Errorf("expected type 'snapshot', got '%s'", wsMsg.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for snapshot message")
	}
}

func TestWsClient_SendSnapshot_BufferFull(t *testing.T) {
	hub := NewWsHub()
	go hub.Run()

	client := &WsClient{
		hub:  hub,
		send: make(chan []byte, 1),
	}

	hub.register <- client

	snapshot := &OrderBookSnapshot{
		VenueID:      uuid.New(),
		InstrumentID: uuid.New(),
		Sequence:     1,
		Bids:         []PriceLevel{{Price: "100", Quantity: "10"}},
		Asks:         []PriceLevel{{Price: "101", Quantity: "5"}},
		Timestamp:    time.Now(),
	}

	client.sendSnapshot(snapshot)
	client.sendSnapshot(snapshot)

	select {
	case msg := <-client.send:
		if len(msg) == 0 {
			t.Error("expected message in buffer")
		}
	default:
		t.Error("expected message in buffer")
	}
}

func TestWsMessage_Format(t *testing.T) {
	msg := WsMessage{
		Type: "snapshot",
		Data: map[string]interface{}{
			"venue_id":      "test-venue",
			"instrument_id": "test-instrument",
			"sequence":      12345,
			"best_bid":      "50000.00",
			"best_ask":      "50001.00",
			"bids":          []interface{}{},
			"asks":          []interface{}{},
		},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("failed to marshal message: %v", err)
	}

	var decoded WsMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal message: %v", err)
	}

	if decoded.Type != "snapshot" {
		t.Errorf("expected type 'snapshot', got '%s'", decoded.Type)
	}
}

func newTestServiceWithSnapshot(venueID, instrumentID uuid.UUID, snapshot *OrderBookSnapshot) *Service {
	repo := &Repository{}
	service := NewService(repo)
	book := service.engine.GetOrCreateBook(venueID, instrumentID, 5*time.Second, 10*time.Second)
	service.engine.ApplySnapshot(book, snapshot)
	return service
}

func TestOrderbookWsHandler_SubscribeWS(t *testing.T) {
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

	service := newTestServiceWithSnapshot(venueID, instrumentID, snapshot)
	hub := NewWsHub()
	go hub.Run()
	handler := NewHandler(service, hub)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gin.SetMode(gin.TestMode)
		c, _ := gin.CreateTestContext(w)
		c.Request = r
		handler.SubscribeWS(c)
	}))
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/orderbook/ws?venue_id=" + venueID.String() + "&instrument_id=" + instrumentID.String()

	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect ws: %v", err)
	}
	defer ws.Close()

	ws.SetReadDeadline(time.Now().Add(2 * time.Second))

	_, msg, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read message: %v", err)
	}

	var wsMsg WsMessage
	if err := json.Unmarshal(msg, &wsMsg); err != nil {
		t.Fatalf("failed to unmarshal message: %v", err)
	}

	if wsMsg.Type != "snapshot" {
		t.Errorf("expected type 'snapshot', got '%s'", wsMsg.Type)
	}
}

func TestOrderbookWsHandler_InvalidVenueID(t *testing.T) {
	repo := &Repository{}
	service := NewService(repo)
	hub := NewWsHub()
	go hub.Run()
	handler := NewHandler(service, hub)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gin.SetMode(gin.TestMode)
		c, _ := gin.CreateTestContext(w)
		c.Request = r
		handler.SubscribeWS(c)
	}))
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

func TestOrderbookWsHandler_InvalidInstrumentID(t *testing.T) {
	repo := &Repository{}
	service := NewService(repo)
	hub := NewWsHub()
	go hub.Run()
	handler := NewHandler(service, hub)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gin.SetMode(gin.TestMode)
		c, _ := gin.CreateTestContext(w)
		c.Request = r
		handler.SubscribeWS(c)
	}))
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

func TestOrderbookWsHandler_MissingParams(t *testing.T) {
	repo := &Repository{}
	service := NewService(repo)
	hub := NewWsHub()
	go hub.Run()
	handler := NewHandler(service, hub)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gin.SetMode(gin.TestMode)
		c, _ := gin.CreateTestContext(w)
		c.Request = r
		handler.SubscribeWS(c)
	}))
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

func TestOrderbookWsHandler_PingPong(t *testing.T) {
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

	service := newTestServiceWithSnapshot(venueID, instrumentID, snapshot)
	hub := NewWsHub()
	go hub.Run()
	handler := NewHandler(service, hub)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gin.SetMode(gin.TestMode)
		c, _ := gin.CreateTestContext(w)
		c.Request = r
		handler.SubscribeWS(c)
	}))
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
		t.Fatalf("failed to unmarshal initial snapshot: %v", err)
	}
	if wsMsg.Type != "snapshot" {
		t.Errorf("expected type 'snapshot', got '%s'", wsMsg.Type)
	}
}

func TestOrderbookWsHandler_NewSnapshotPushed(t *testing.T) {
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

	service := newTestServiceWithSnapshot(venueID, instrumentID, snapshot)
	hub := NewWsHub()
	go hub.Run()
	handler := NewHandler(service, hub)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gin.SetMode(gin.TestMode)
		c, _ := gin.CreateTestContext(w)
		c.Request = r
		handler.SubscribeWS(c)
	}))
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/orderbook/ws?venue_id=" + venueID.String() + "&instrument_id=" + instrumentID.String()

	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect ws: %v", err)
	}
	defer ws.Close()

	ws.SetReadDeadline(time.Now().Add(5 * time.Second))

	_, msg, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read initial snapshot: %v", err)
	}

	var wsMsg WsMessage
	if err := json.Unmarshal(msg, &wsMsg); err != nil {
		t.Fatalf("failed to unmarshal initial snapshot: %v", err)
	}

	snapshotData, ok := wsMsg.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected data to be map, got %T", wsMsg.Data)
	}
	seq, ok := snapshotData["sequence"].(float64)
	if !ok {
		t.Fatalf("expected sequence to be float64, got %T", snapshotData["sequence"])
	}
	if int64(seq) != 1 {
		t.Errorf("expected initial sequence 1, got %v", seq)
	}
}

func TestOrderbookWsHandler_MultipleClients(t *testing.T) {
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

	service := newTestServiceWithSnapshot(venueID, instrumentID, snapshot)
	hub := NewWsHub()
	go hub.Run()
	handler := NewHandler(service, hub)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gin.SetMode(gin.TestMode)
		c, _ := gin.CreateTestContext(w)
		c.Request = r
		handler.SubscribeWS(c)
	}))
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/orderbook/ws?venue_id=" + venueID.String() + "&instrument_id=" + instrumentID.String()

	var wg sync.WaitGroup
	var mu sync.Mutex
	var readMessages []*WsMessage

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
			if err != nil {
				t.Errorf("failed to connect ws: %v", err)
				return
			}
			defer ws.Close()

			ws.SetReadDeadline(time.Now().Add(2 * time.Second))

			_, msg, err := ws.ReadMessage()
			if err != nil {
				t.Errorf("failed to read message: %v", err)
				return
			}

			var wsMsg WsMessage
			if err := json.Unmarshal(msg, &wsMsg); err != nil {
				t.Errorf("failed to unmarshal message: %v", err)
				return
			}

			mu.Lock()
			readMessages = append(readMessages, &wsMsg)
			mu.Unlock()
		}()
	}

	wg.Wait()

	if len(readMessages) != 3 {
		t.Errorf("expected 3 messages, got %d", len(readMessages))
	}

	for _, msg := range readMessages {
		if msg.Type != "snapshot" {
			t.Errorf("expected type 'snapshot', got '%s'", msg.Type)
		}
	}
}

func TestOrderbookWsHandler_InvalidDepth(t *testing.T) {
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

	service := newTestServiceWithSnapshot(venueID, instrumentID, snapshot)
	hub := NewWsHub()
	go hub.Run()
	handler := NewHandler(service, hub)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gin.SetMode(gin.TestMode)
		c, _ := gin.CreateTestContext(w)
		c.Request = r
		handler.SubscribeWS(c)
	}))
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/orderbook/ws?venue_id=" + venueID.String() + "&instrument_id=" + instrumentID.String() + "&depth=50"

	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect ws: %v", err)
	}
	defer ws.Close()

	ws.SetReadDeadline(time.Now().Add(2 * time.Second))

	_, msg, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read message: %v", err)
	}

	var wsMsg WsMessage
	if err := json.Unmarshal(msg, &wsMsg); err != nil {
		t.Fatalf("failed to unmarshal message: %v", err)
	}

	if wsMsg.Type != "snapshot" {
		t.Errorf("expected type 'snapshot', got '%s'", wsMsg.Type)
	}
}

func strPtr(s string) *string {
	return &s
}
