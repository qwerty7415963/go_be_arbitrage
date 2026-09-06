package market

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestConnectionManager_CreateConnection(t *testing.T) {
	cm := NewConnectionManager()

	conn := cm.CreateConnection("binance")

	if conn == nil {
		t.Fatal("expected connection to be created")
	}
	if conn.VenueCode != "binance" {
		t.Errorf("expected venue code binance, got %s", conn.VenueCode)
	}
	if conn.State != ConnectionStateDisconnected {
		t.Errorf("expected state DISCONNECTED, got %s", conn.State)
	}
}

func TestConnectionManager_UpdateState(t *testing.T) {
	cm := NewConnectionManager()

	conn := cm.CreateConnection("binance")
	cm.UpdateState(conn.ID, ConnectionStateConnected)

	if cm.GetState(conn.ID) != ConnectionStateConnected {
		t.Error("expected state to be CONNECTED")
	}

	cm.UpdateState(conn.ID, ConnectionStateDisconnected)

	if cm.GetState(conn.ID) != ConnectionStateDisconnected {
		t.Error("expected state to be DISCONNECTED")
	}
}

func TestConnectionManager_IsConnected(t *testing.T) {
	cm := NewConnectionManager()

	conn := cm.CreateConnection("binance")

	if cm.IsConnected(conn.ID) {
		t.Error("expected not connected initially")
	}

	cm.UpdateState(conn.ID, ConnectionStateConnected)

	if !cm.IsConnected(conn.ID) {
		t.Error("expected connected after update")
	}
}

func TestConnectionManager_GetVenueConnections(t *testing.T) {
	cm := NewConnectionManager()

	conn1 := cm.CreateConnection("binance")
	conn2 := cm.CreateConnection("binance")
	conn3 := cm.CreateConnection("okx")

	binanceConns := cm.GetVenueConnections("binance")

	if len(binanceConns) != 2 {
		t.Errorf("expected 2 connections for binance, got %d", len(binanceConns))
	}

	okxConns := cm.GetVenueConnections("okx")

	if len(okxConns) != 1 {
		t.Errorf("expected 1 connection for okx, got %d", len(okxConns))
	}

	_ = conn1
	_ = conn2
	_ = conn3
}

func TestConnectionManager_RemoveConnection(t *testing.T) {
	cm := NewConnectionManager()

	conn := cm.CreateConnection("binance")
	cm.RemoveConnection(conn.ID)

	if _, ok := cm.GetConnection(conn.ID); ok {
		t.Error("expected connection to be removed")
	}
}

func TestBackoffCalculator_Calculate(t *testing.T) {
	config := DefaultConnectionConfig()
	bc := NewBackoffCalculator(config)

	// Test backoff calculation
	backoff0 := bc.Calculate(0)
	backoff1 := bc.Calculate(1)
	backoff2 := bc.Calculate(2)

	if backoff0 != 1*time.Second {
		t.Errorf("expected 1s, got %v", backoff0)
	}
	if backoff1 != 2*time.Second {
		t.Errorf("expected 2s, got %v", backoff1)
	}
	if backoff2 != 4*time.Second {
		t.Errorf("expected 4s, got %v", backoff2)
	}
}

func TestBackoffCalculator_MaxBackoff(t *testing.T) {
	config := DefaultConnectionConfig()
	bc := NewBackoffCalculator(config)

	// Test max backoff
	backoff := bc.Calculate(100)

	if backoff > config.MaxBackoff {
		t.Errorf("expected max backoff %v, got %v", config.MaxBackoff, backoff)
	}
}

func TestSubscriptionManager_Subscribe(t *testing.T) {
	sm := NewSubscriptionManager()

	venueID := uuid.New()
	instrumentID := uuid.New()
	connectionID := uuid.New()

	sub := sm.Subscribe(venueID, instrumentID, "trades", connectionID)

	if sub == nil {
		t.Fatal("expected subscription to be created")
	}
	if sub.VenueID != venueID {
		t.Errorf("expected venue ID %s, got %s", venueID, sub.VenueID)
	}
	if sub.InstrumentID != instrumentID {
		t.Errorf("expected instrument ID %s, got %s", instrumentID, sub.InstrumentID)
	}
	if sub.Channel != "trades" {
		t.Errorf("expected channel trades, got %s", sub.Channel)
	}
	if sub.Status != SubscriptionStatusPending {
		t.Errorf("expected status PENDING, got %s", sub.Status)
	}
}

func TestSubscriptionManager_UpdateStatus(t *testing.T) {
	sm := NewSubscriptionManager()

	sub := sm.Subscribe(uuid.New(), uuid.New(), "trades", uuid.New())
	sm.UpdateStatus(sub.ID, SubscriptionStatusActive)

	s, ok := sm.GetSubscription(sub.ID)
	if !ok || s == nil {
		t.Fatal("expected subscription to exist")
	}
	if s.Status != SubscriptionStatusActive {
		t.Errorf("expected status ACTIVE, got %s", s.Status)
	}
}

func TestSubscriptionManager_Unsubscribe(t *testing.T) {
	sm := NewSubscriptionManager()

	sub := sm.Subscribe(uuid.New(), uuid.New(), "trades", uuid.New())
	sm.Unsubscribe(sub.ID)

	s, ok := sm.GetSubscription(sub.ID)
	if !ok || s == nil {
		t.Fatal("expected subscription to exist")
	}
	if s.Status != SubscriptionStatusUnsubscribed {
		t.Errorf("expected status UNSUBSCRIBED, got %s", s.Status)
	}
}

func TestSubscriptionManager_GetVenueSubscriptions(t *testing.T) {
	sm := NewSubscriptionManager()

	venueID := uuid.New()
	sm.Subscribe(venueID, uuid.New(), "trades", uuid.New())
	sm.Subscribe(venueID, uuid.New(), "ticker", uuid.New())
	sm.Subscribe(uuid.New(), uuid.New(), "trades", uuid.New())

	subs := sm.GetVenueSubscriptions(venueID)

	if len(subs) != 2 {
		t.Errorf("expected 2 subscriptions for venue, got %d", len(subs))
	}
}

func TestSubscriptionManager_GetChannelSubscriptions(t *testing.T) {
	sm := NewSubscriptionManager()

	sm.Subscribe(uuid.New(), uuid.New(), "trades", uuid.New())
	sm.Subscribe(uuid.New(), uuid.New(), "trades", uuid.New())
	sm.Subscribe(uuid.New(), uuid.New(), "ticker", uuid.New())

	subs := sm.GetChannelSubscriptions("trades")

	if len(subs) != 2 {
		t.Errorf("expected 2 subscriptions for trades channel, got %d", len(subs))
	}
}

func TestSubscriptionManager_GetActiveSubscriptions(t *testing.T) {
	sm := NewSubscriptionManager()

	sub1 := sm.Subscribe(uuid.New(), uuid.New(), "trades", uuid.New())
	sub2 := sm.Subscribe(uuid.New(), uuid.New(), "trades", uuid.New())
	sm.Subscribe(uuid.New(), uuid.New(), "ticker", uuid.New())

	sm.UpdateStatus(sub1.ID, SubscriptionStatusActive)
	sm.UpdateStatus(sub2.ID, SubscriptionStatusActive)

	subs := sm.GetActiveSubscriptions()

	if len(subs) != 2 {
		t.Errorf("expected 2 active subscriptions, got %d", len(subs))
	}
}

func TestParseTradeEvent(t *testing.T) {
	data := []byte(`{
		"venue_id": "550e8400-e29b-41d4-a716-446655440000",
		"instrument_id": "550e8400-e29b-41d4-a716-446655440001",
		"price": "50000.50",
		"quantity": "0.1",
		"side": "BUY"
	}`)

	trade, err := ParseTradeEvent(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if trade.Price != "50000.50" {
		t.Errorf("expected price 50000.50, got %s", trade.Price)
	}
	if trade.Quantity != "0.1" {
		t.Errorf("expected quantity 0.1, got %s", trade.Quantity)
	}
	if trade.Side != "BUY" {
		t.Errorf("expected side BUY, got %s", trade.Side)
	}
}

func TestParseTickerEvent(t *testing.T) {
	data := []byte(`{
		"venue_id": "550e8400-e29b-41d4-a716-446655440000",
		"instrument_id": "550e8400-e29b-41d4-a716-446655440001",
		"best_bid_price": "49999.00",
		"best_ask_price": "50001.00"
	}`)

	ticker, err := ParseTickerEvent(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ticker.BestBidPrice == nil || *ticker.BestBidPrice != "49999.00" {
		t.Errorf("expected best_bid_price 49999.00, got %v", ticker.BestBidPrice)
	}
	if ticker.BestAskPrice == nil || *ticker.BestAskPrice != "50001.00" {
		t.Errorf("expected best_ask_price 50001.00, got %v", ticker.BestAskPrice)
	}
}

func TestParseFundingEvent(t *testing.T) {
	data := []byte(`{
		"venue_id": "550e8400-e29b-41d4-a716-446655440000",
		"instrument_id": "550e8400-e29b-41d4-a716-446655440001",
		"funding_rate": "0.0001",
		"interval_seconds": 28800
	}`)

	funding, err := ParseFundingEvent(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if funding.FundingRate != "0.0001" {
		t.Errorf("expected funding_rate 0.0001, got %s", funding.FundingRate)
	}
	if funding.IntervalSeconds != 28800 {
		t.Errorf("expected interval_seconds 28800, got %d", funding.IntervalSeconds)
	}
}

func TestNormalizeTimestamp(t *testing.T) {
	// Test seconds
	tsSeconds := int64(1609459200)
	normalized := NormalizeTimestamp(tsSeconds)

	if normalized.Year() != 2021 {
		t.Errorf("expected year 2021, got %d", normalized.Year())
	}

	// Test milliseconds
	tsMillis := int64(1609459200000)
	normalized = NormalizeTimestamp(tsMillis)

	if normalized.Year() != 2021 {
		t.Errorf("expected year 2021, got %d", normalized.Year())
	}
}

func TestDefaultConnectionConfig(t *testing.T) {
	config := DefaultConnectionConfig()

	if config.MaxReconnect != 10 {
		t.Errorf("expected max reconnect 10, got %d", config.MaxReconnect)
	}
	if config.InitialBackoff != 1*time.Second {
		t.Errorf("expected initial backoff 1s, got %v", config.InitialBackoff)
	}
	if config.MaxBackoff != 30*time.Second {
		t.Errorf("expected max backoff 30s, got %v", config.MaxBackoff)
	}
	if config.BackoffMultiplier != 2.0 {
		t.Errorf("expected backoff multiplier 2.0, got %f", config.BackoffMultiplier)
	}
}
