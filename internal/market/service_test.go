package market

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

func newTestServiceNoDB() *Service {
	return NewService(
		nil,
		nil,
		NewConnectionManager(),
		NewSubscriptionManager(),
	)
}

func TestService_Subscribe_CreatesSubscription(t *testing.T) {
	svc := newTestServiceNoDB()
	ctx := context.Background()
	venueID := uuid.New()
	instrumentID := uuid.New()
	connectionID := uuid.New()

	sub, err := svc.Subscribe(ctx, venueID, instrumentID, "trades", connectionID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
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
	if sub.Status != SubscriptionStatusActive {
		t.Errorf("expected status ACTIVE, got %s", sub.Status)
	}
}

func TestService_Unsubscribe_DeactivatesSubscription(t *testing.T) {
	svc := newTestServiceNoDB()
	ctx := context.Background()

	sub, _ := svc.Subscribe(ctx, uuid.New(), uuid.New(), "trades", uuid.New())
	err := svc.Unsubscribe(ctx, sub.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s, ok := svc.subManager.GetSubscription(sub.ID)
	if !ok || s == nil {
		t.Fatal("expected subscription to still exist")
	}
	if s.Status != SubscriptionStatusUnsubscribed {
		t.Errorf("expected status UNSUBSCRIBED, got %s", s.Status)
	}
}

func TestService_GetActiveSubscriptions_ReturnsActiveOnly(t *testing.T) {
	svc := newTestServiceNoDB()
	ctx := context.Background()

	sub1, _ := svc.Subscribe(ctx, uuid.New(), uuid.New(), "trades", uuid.New())
	svc.Subscribe(ctx, uuid.New(), uuid.New(), "ticker", uuid.New())
	svc.Unsubscribe(ctx, sub1.ID)

	active := svc.GetActiveSubscriptions()
	if len(active) != 1 {
		t.Errorf("expected 1 active subscription, got %d", len(active))
	}
}

func TestService_GetVenueSubscriptions_FiltersByVenue(t *testing.T) {
	svc := newTestServiceNoDB()
	ctx := context.Background()

	venueID := uuid.New()
	otherVenueID := uuid.New()

	svc.Subscribe(ctx, venueID, uuid.New(), "trades", uuid.New())
	svc.Subscribe(ctx, venueID, uuid.New(), "ticker", uuid.New())
	svc.Subscribe(ctx, otherVenueID, uuid.New(), "trades", uuid.New())

	subs := svc.GetVenueSubscriptions(venueID)
	if len(subs) != 2 {
		t.Errorf("expected 2 subscriptions for venue, got %d", len(subs))
	}
}

func TestService_CreateConnection_ReturnsConnection(t *testing.T) {
	svc := newTestServiceNoDB()

	conn := svc.CreateConnection("binance")
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

func TestService_GetConnection_ReturnsExisting(t *testing.T) {
	svc := newTestServiceNoDB()

	conn := svc.CreateConnection("binance")
	got, ok := svc.GetConnection(conn.ID)
	if !ok {
		t.Fatal("expected connection to be found")
	}
	if got.ID != conn.ID {
		t.Errorf("expected ID %s, got %s", conn.ID, got.ID)
	}
}

func TestService_GetConnection_WhenNotFound(t *testing.T) {
	svc := newTestServiceNoDB()

	_, ok := svc.GetConnection(uuid.New())
	if ok {
		t.Error("expected connection not found")
	}
}

func TestService_IsConnected_ReturnsFalse_WhenDisconnected(t *testing.T) {
	svc := newTestServiceNoDB()

	conn := svc.CreateConnection("binance")
	if svc.IsConnected(conn.ID) {
		t.Error("expected not connected")
	}
}

func TestService_IsConnected_ReturnsTrue_WhenConnected(t *testing.T) {
	svc := newTestServiceNoDB()

	conn := svc.CreateConnection("binance")
	svc.connManager.UpdateState(conn.ID, ConnectionStateConnected)
	if !svc.IsConnected(conn.ID) {
		t.Error("expected connected")
	}
}

func TestService_Subscribe_MultipleChannels(t *testing.T) {
	svc := newTestServiceNoDB()
	ctx := context.Background()
	venueID := uuid.New()
	instrumentID := uuid.New()
	connID := uuid.New()

	channels := []string{"trades", "ticker", "funding"}
	for _, ch := range channels {
		sub, err := svc.Subscribe(ctx, venueID, instrumentID, ch, connID)
		if err != nil {
			t.Fatalf("unexpected error for channel %s: %v", ch, err)
		}
		if sub.Channel != ch {
			t.Errorf("expected channel %s, got %s", ch, sub.Channel)
		}
	}

	subs := svc.GetVenueSubscriptions(venueID)
	if len(subs) != 3 {
		t.Errorf("expected 3 subscriptions, got %d", len(subs))
	}
}

func TestParseTradeEvent_InvalidJSON_ReturnsError(t *testing.T) {
	_, err := ParseTradeEvent([]byte("not json"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParseTickerEvent_InvalidJSON_ReturnsError(t *testing.T) {
	_, err := ParseTickerEvent([]byte("{invalid"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParseFundingEvent_InvalidJSON_ReturnsError(t *testing.T) {
	_, err := ParseFundingEvent([]byte("}bad{"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParseTradeEvent_EmptyBody_ReturnsError(t *testing.T) {
	_, err := ParseTradeEvent([]byte{})
	if err == nil {
		t.Error("expected error for empty body")
	}
}

func TestParseTickerEvent_EmptyBody_ReturnsError(t *testing.T) {
	_, err := ParseTickerEvent([]byte{})
	if err == nil {
		t.Error("expected error for empty body")
	}
}

func TestParseFundingEvent_EmptyBody_ReturnsError(t *testing.T) {
	_, err := ParseFundingEvent([]byte{})
	if err == nil {
		t.Error("expected error for empty body")
	}
}

func TestParseTradeEvent_PartialFields(t *testing.T) {
	data := []byte(`{"price":"50000","side":"BUY"}`)
	trade, err := ParseTradeEvent(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if trade.Price != "50000" {
		t.Errorf("expected price 50000, got %s", trade.Price)
	}
}

func TestParseTickerEvent_PartialFields(t *testing.T) {
	data := []byte(`{"best_bid_price":"49999"}`)
	ticker, err := ParseTickerEvent(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ticker.BestBidPrice == nil || *ticker.BestBidPrice != "49999" {
		t.Errorf("expected best_bid_price 49999, got %v", ticker.BestBidPrice)
	}
}

func TestParseFundingEvent_PartialFields(t *testing.T) {
	data := []byte(`{"funding_rate":"0.001"}`)
	funding, err := ParseFundingEvent(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if funding.FundingRate != "0.001" {
		t.Errorf("expected funding_rate 0.001, got %s", funding.FundingRate)
	}
}

func TestNormalizeTimestamp_Seconds(t *testing.T) {
	ts := int64(1609459200)
	result := NormalizeTimestamp(ts)
	expected := time.Unix(ts, 0)
	if !result.Equal(expected) {
		t.Errorf("expected %v, got %v", expected, result)
	}
}

func TestNormalizeTimestamp_Milliseconds(t *testing.T) {
	ts := int64(1609459200000)
	result := NormalizeTimestamp(ts)
	expected := time.UnixMilli(ts)
	if !result.Equal(expected) {
		t.Errorf("expected %v, got %v", expected, result)
	}
}

func TestNormalizeTimestamp_Zero(t *testing.T) {
	result := NormalizeTimestamp(0)
	expected := time.Unix(0, 0)
	if !result.Equal(expected) {
		t.Errorf("expected %v, got %v", expected, result)
	}
}

func TestParseTradeEvent_SerializeDeserialize(t *testing.T) {
	original, _ := ParseTradeEvent([]byte(`{
		"venue_id": "550e8400-e29b-41d4-a716-446655440000",
		"instrument_id": "550e8400-e29b-41d4-a716-446655440001",
		"price": "50000.50",
		"quantity": "0.1",
		"side": "BUY"
	}`))

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	parsed, err := ParseTradeEvent(data)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if parsed.Price != original.Price {
		t.Errorf("price mismatch: %s vs %s", parsed.Price, original.Price)
	}
	if parsed.Side != original.Side {
		t.Errorf("side mismatch: %s vs %s", parsed.Side, original.Side)
	}
}

func TestService_ProcessTrade_WithNilRepo_Panics(t *testing.T) {
	svc := newTestServiceNoDB()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic when normalizedRepo is nil")
		}
	}()

	trade := &TradeEvent{
		VenueID:      uuid.New(),
		InstrumentID: uuid.New(),
		Price:        "50000",
		Quantity:     "0.1",
		Side:         "BUY",
	}
	_ = svc.ProcessTrade(context.Background(), trade, nil)
}

func TestService_ProcessTicker_WithNilRepo_Panics(t *testing.T) {
	svc := newTestServiceNoDB()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic when normalizedRepo is nil")
		}
	}()

	ticker := &TickerEvent{
		VenueID:      uuid.New(),
		InstrumentID: uuid.New(),
	}
	_ = svc.ProcessTicker(context.Background(), ticker, nil)
}

func TestService_ProcessFunding_WithNilRepo_Panics(t *testing.T) {
	svc := newTestServiceNoDB()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic when normalizedRepo is nil")
		}
	}()

	funding := &FundingEvent{
		VenueID:      uuid.New(),
		InstrumentID: uuid.New(),
		FundingRate:  "0.0001",
	}
	_ = svc.ProcessFunding(context.Background(), funding, nil)
}

func TestService_ProcessRawEvent_WithNilRepo_Panics(t *testing.T) {
	svc := newTestServiceNoDB()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic when rawRepo is nil")
		}
	}()

	event := &RawMarketEvent{
		VenueID:  uuid.New(),
		Payload:  []byte(`{}`),
	}
	_ = svc.ProcessRawEvent(context.Background(), event)
}
