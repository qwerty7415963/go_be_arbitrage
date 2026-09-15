package exchange

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestFormatExtendedMarket(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"BTCUSD", "BTC-USD"},
		{"ETHUSD", "ETH-USD"},
		{"SOLEUSD", "SOLE-USD"},
	}

	for _, tt := range tests {
		got := FormatExtendedMarket(tt.input)
		if got != tt.expected {
			t.Errorf("FormatExtendedMarket(%s) = %s, want %s", tt.input, got, tt.expected)
		}
	}
}

func TestParseExtendedMarket(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"BTC-USD", "BTCUSD"},
		{"ETH-USD", "ETHUSD"},
		{"SOLE-USD", "SOLEUSD"},
	}

	for _, tt := range tests {
		got := ParseExtendedMarket(tt.input)
		if got != tt.expected {
			t.Errorf("ParseExtendedMarket(%s) = %s, want %s", tt.input, got, tt.expected)
		}
	}
}

func TestExtendedWSAdapter_ParseSnapshotMessage(t *testing.T) {
	raw := []byte(`{"ts":1701563440000,"type":"SNAPSHOT","data":{"m":"BTC-USD","b":[{"p":"42000","q":"1.5","c":"1.5"},{"p":"41999","q":"2.0","c":"2.0"}],"a":[{"p":"42001","q":"0.8","c":"0.8"},{"p":"42002","q":"1.2","c":"1.2"}]},"seq":1}`)

	var msg ExtendedDepthMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if msg.Type != "SNAPSHOT" {
		t.Errorf("expected type SNAPSHOT, got %s", msg.Type)
	}
	if msg.Seq != 1 {
		t.Errorf("expected seq 1, got %d", msg.Seq)
	}
	if msg.Data.M != "BTC-USD" {
		t.Errorf("expected market BTC-USD, got %s", msg.Data.M)
	}
	if len(msg.Data.B) != 2 {
		t.Errorf("expected 2 bids, got %d", len(msg.Data.B))
	}
	if len(msg.Data.A) != 2 {
		t.Errorf("expected 2 asks, got %d", len(msg.Data.A))
	}
	if msg.Data.B[0].P != "42000" {
		t.Errorf("expected best bid price 42000, got %s", msg.Data.B[0].P)
	}
	if msg.Data.B[0].C != "1.5" {
		t.Errorf("expected best bid qty 1.5, got %s", msg.Data.B[0].C)
	}
}

func TestExtendedWSAdapter_ParseDeltaMessage(t *testing.T) {
	raw := []byte(`{"ts":1701563441000,"type":"DELTA","data":{"m":"BTC-USD","b":[{"p":"42000","q":"-0.5","c":"1.0"}],"a":[{"p":"42001","q":"0.3","c":"1.1"}]},"seq":2}`)

	var msg ExtendedDepthMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if msg.Type != "DELTA" {
		t.Errorf("expected type DELTA, got %s", msg.Type)
	}
	if msg.Seq != 2 {
		t.Errorf("expected seq 2, got %d", msg.Seq)
	}
	if msg.Data.B[0].Q != "-0.5" {
		t.Errorf("expected bid change -0.5, got %s", msg.Data.B[0].Q)
	}
	if msg.Data.B[0].C != "1.0" {
		t.Errorf("expected bid absolute 1.0, got %s", msg.Data.B[0].C)
	}
}

func TestExtendedWSAdapter_HandleMessage_Snapshot(t *testing.T) {
	adapter := NewExtendedWSAdapter(uuid.New())
	eventChan := make(chan *OrderBookEvent, 10)

	raw := []byte(`{"ts":1701563440000,"type":"SNAPSHOT","data":{"m":"BTC-USD","b":[{"p":"42000","q":"1.5","c":"1.5"}],"a":[{"p":"42001","q":"0.8","c":"0.8"}]},"seq":1}`)

	adapter.handleMessage("BTCUSD", raw, eventChan)

	event := <-eventChan

	if !event.IsSnapshot {
		t.Error("expected snapshot event")
	}
	if event.Sequence != 1 {
		t.Errorf("expected sequence 1, got %d", event.Sequence)
	}
	if len(event.Bids) != 1 {
		t.Fatalf("expected 1 bid, got %d", len(event.Bids))
	}
	if event.Bids[0].Price != "42000" {
		t.Errorf("expected price 42000, got %s", event.Bids[0].Price)
	}
	if event.Bids[0].Quantity != "1.5" {
		t.Errorf("expected quantity 1.5, got %s", event.Bids[0].Quantity)
	}
	if len(event.Asks) != 1 {
		t.Fatalf("expected 1 ask, got %d", len(event.Asks))
	}
	if event.Asks[0].Price != "42001" {
		t.Errorf("expected price 42001, got %s", event.Asks[0].Price)
	}
}

func TestExtendedWSAdapter_HandleMessage_Delta(t *testing.T) {
	adapter := NewExtendedWSAdapter(uuid.New())
	eventChan := make(chan *OrderBookEvent, 10)

	raw := []byte(`{"ts":1701563440000,"type":"SNAPSHOT","data":{"m":"ETH-USD","b":[{"p":"2200","q":"10","c":"10"}],"a":[{"p":"2201","q":"5","c":"5"}]},"seq":1}`)
	adapter.handleMessage("ETHUSD", raw, eventChan)
	<-eventChan

	raw = []byte(`{"ts":1701563441000,"type":"DELTA","data":{"m":"ETH-USD","b":[{"p":"2200","q":"-3","c":"7"}],"a":[{"p":"2201","q":"2","c":"7"}]},"seq":2}`)
	adapter.handleMessage("ETHUSD", raw, eventChan)

	event := <-eventChan

	if event.IsSnapshot {
		t.Error("expected delta event, not snapshot")
	}
	if event.Sequence != 2 {
		t.Errorf("expected sequence 2, got %d", event.Sequence)
	}
	if event.FromSequence != 1 {
		t.Errorf("expected from_sequence 1, got %d", event.FromSequence)
	}
	if event.Bids[0].Quantity != "7" {
		t.Errorf("expected absolute qty 7, got %s", event.Bids[0].Quantity)
	}
}

func TestExtendedWSAdapter_HandleMessage_DuplicateIgnored(t *testing.T) {
	adapter := NewExtendedWSAdapter(uuid.New())
	eventChan := make(chan *OrderBookEvent, 10)

	raw := []byte(`{"ts":1701563440000,"type":"SNAPSHOT","data":{"m":"BTC-USD","b":[{"p":"42000","q":"1","c":"1"}],"a":[{"p":"42001","q":"1","c":"1"}]},"seq":5}`)
	adapter.handleMessage("BTCUSD", raw, eventChan)
	<-eventChan

	raw = []byte(`{"ts":1701563440000,"type":"DELTA","data":{"m":"BTC-USD","b":[{"p":"42000","q":"2","c":"2"}],"a":[]},"seq":3}`)
	adapter.handleMessage("BTCUSD", raw, eventChan)

	select {
	case <-eventChan:
		t.Error("expected duplicate to be ignored")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestExtendedWSAdapter_HandleMessage_OutOfOrderIgnored(t *testing.T) {
	adapter := NewExtendedWSAdapter(uuid.New())
	eventChan := make(chan *OrderBookEvent, 10)

	raw := []byte(`{"ts":1701563440000,"type":"SNAPSHOT","data":{"m":"BTC-USD","b":[{"p":"42000","q":"1","c":"1"}],"a":[{"p":"42001","q":"1","c":"1"}]},"seq":5}`)
	adapter.handleMessage("BTCUSD", raw, eventChan)
	<-eventChan

	raw = []byte(`{"ts":1701563441000,"type":"DELTA","data":{"m":"BTC-USD","b":[{"p":"42000","q":"2","c":"2"}],"a":[]},"seq":4}`)
	adapter.handleMessage("BTCUSD", raw, eventChan)

	select {
	case <-eventChan:
		t.Error("expected out-of-order to be ignored")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestExtendedWSAdapter_HandleMessage_EmptyBook(t *testing.T) {
	adapter := NewExtendedWSAdapter(uuid.New())
	eventChan := make(chan *OrderBookEvent, 10)

	raw := []byte(`{"ts":1701563440000,"type":"SNAPSHOT","data":{"m":"SOL-USD","b":[],"a":[]},"seq":1}`)
	adapter.handleMessage("SOLUSD", raw, eventChan)

	event := <-eventChan

	if !event.IsSnapshot {
		t.Error("expected snapshot")
	}
	if len(event.Bids) != 0 {
		t.Errorf("expected 0 bids, got %d", len(event.Bids))
	}
	if len(event.Asks) != 0 {
		t.Errorf("expected 0 asks, got %d", len(event.Asks))
	}
}

func TestExtendedWSAdapter_HandleMessage_MultipleLevels(t *testing.T) {
	adapter := NewExtendedWSAdapter(uuid.New())
	eventChan := make(chan *OrderBookEvent, 10)

	raw := []byte(`{"ts":1701563440000,"type":"SNAPSHOT","data":{"m":"BTC-USD","b":[{"p":"42000","q":"1","c":"1"},{"p":"41999","q":"2","c":"2"},{"p":"41998","q":"3","c":"3"}],"a":[{"p":"42001","q":"1","c":"1"},{"p":"42002","q":"2","c":"2"},{"p":"42003","q":"3","c":"3"}]},"seq":1}`)
	adapter.handleMessage("BTCUSD", raw, eventChan)

	event := <-eventChan

	if len(event.Bids) != 3 {
		t.Fatalf("expected 3 bids, got %d", len(event.Bids))
	}
	if len(event.Asks) != 3 {
		t.Fatalf("expected 3 asks, got %d", len(event.Asks))
	}
	if event.Bids[0].Price != "42000" {
		t.Errorf("expected best bid 42000, got %s", event.Bids[0].Price)
	}
	if event.Bids[2].Price != "41998" {
		t.Errorf("expected worst bid 41998, got %s", event.Bids[2].Price)
	}
	if event.Asks[0].Price != "42001" {
		t.Errorf("expected best ask 42001, got %s", event.Asks[0].Price)
	}
}

func TestExtendedWSAdapter_Disconnect(t *testing.T) {
	adapter := NewExtendedWSAdapter(uuid.New())
	_ = adapter.IsConnected()

	err := adapter.Disconnect(nil)
	if err != nil {
		t.Errorf("unexpected disconnect error: %v", err)
	}
}
