package exchange

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	extendedWSBaseURL = "wss://api.starknet.extended.exchange/stream.extended.exchange/v1"
)

type ExtendedWSAdapter struct {
	venueID      uuid.UUID
	wsClients    map[string]*WSClient
	eventChans   map[string]chan *OrderBookEvent
	lastSeq      map[string]int64
	mu           sync.RWMutex
}

func NewExtendedWSAdapter(venueID uuid.UUID) *ExtendedWSAdapter {
	return &ExtendedWSAdapter{
		venueID:    venueID,
		wsClients:  make(map[string]*WSClient),
		eventChans: make(map[string]chan *OrderBookEvent),
		lastSeq:    make(map[string]int64),
	}
}

type ExtendedDepthMessage struct {
	Ts   int64  `json:"ts"`
	Type string `json:"type"`
	Data struct {
		M string `json:"m"`
		B []struct {
			P string `json:"p"`
			Q string `json:"q"`
			C string `json:"c"`
		} `json:"b"`
		A []struct {
			P string `json:"p"`
			Q string `json:"q"`
			C string `json:"c"`
		} `json:"a"`
	} `json:"data"`
	Seq int64 `json:"seq"`
}

func (a *ExtendedWSAdapter) SubscribeOrderBook(ctx context.Context, symbol string, depth int) (<-chan *OrderBookEvent, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if ch, ok := a.eventChans[symbol]; ok {
		return ch, nil
	}

	eventChan := make(chan *OrderBookEvent, 100)
	a.eventChans[symbol] = eventChan
	a.lastSeq[symbol] = 0

	market := FormatExtendedMarket(symbol)
	url := fmt.Sprintf("%s/orderbooks/%s", extendedWSBaseURL, market)

	wsClient := NewWSClient(url, DefaultWSClientConfig())
	a.wsClients[symbol] = wsClient

	wsClient.SetCallbacks(
		func(data []byte) { a.handleMessage(symbol, data, eventChan) },
		func() {
			log.Printf("extended ws: connected for %s", symbol)
		},
		func() {
			go func() {
				if err := wsClient.Reconnect(ctx); err != nil {
					log.Printf("extended ws: reconnect failed for %s: %v", symbol, err)
					close(eventChan)
				}
			}()
		},
	)

	if err := wsClient.Connect(ctx); err != nil {
		return nil, fmt.Errorf("connect extended ws: %w", err)
	}

	return eventChan, nil
}

func (a *ExtendedWSAdapter) handleMessage(symbol string, data []byte, eventChan chan *OrderBookEvent) {
	var msg ExtendedDepthMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return
	}

	a.mu.Lock()
	prevSeq := a.lastSeq[symbol]
	a.mu.Unlock()

	if msg.Seq > 0 && prevSeq > 0 && msg.Seq <= prevSeq {
		return
	}

	now := time.Now()
	isSnapshot := msg.Type == "SNAPSHOT"

	bids := make([]PriceLevel, 0, len(msg.Data.B))
	for _, b := range msg.Data.B {
		qty := b.C
		if !isSnapshot && b.Q != "" {
			qty = b.C
		}
		bids = append(bids, PriceLevel{Price: b.P, Quantity: qty})
	}

	asks := make([]PriceLevel, 0, len(msg.Data.A))
	for _, a := range msg.Data.A {
		qty := a.C
		if !isSnapshot && a.Q != "" {
			qty = a.C
		}
		asks = append(asks, PriceLevel{Price: a.P, Quantity: qty})
	}

	fromSeq := prevSeq
	if isSnapshot {
		fromSeq = 0
	}

	event := &OrderBookEvent{
		VenueSymbol:  symbol,
		Bids:         bids,
		Asks:         asks,
		Timestamp:    now,
		IsSnapshot:   isSnapshot,
		Sequence:     msg.Seq,
		FromSequence: fromSeq,
	}

	a.mu.Lock()
	a.lastSeq[symbol] = msg.Seq
	a.mu.Unlock()

	select {
	case eventChan <- event:
	default:
	}
}

func (a *ExtendedWSAdapter) Disconnect(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	for symbol, wsClient := range a.wsClients {
		wsClient.Disconnect()
		delete(a.wsClients, symbol)
	}

	for symbol := range a.eventChans {
		delete(a.eventChans, symbol)
	}
	for symbol := range a.lastSeq {
		delete(a.lastSeq, symbol)
	}

	return nil
}

func (a *ExtendedWSAdapter) IsConnected() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()

	for _, wsClient := range a.wsClients {
		if wsClient.IsConnected() {
			return true
		}
	}
	return false
}

// FormatExtendedMarket converts canonical symbol to Extended market format
// e.g., "BTCUSD" -> "BTC-USD", "ETHUSD" -> "ETH-USD"
func FormatExtendedMarket(canonical string) string {
	if strings.HasSuffix(canonical, "USD") {
		base := strings.TrimSuffix(canonical, "USD")
		return base + "-USD"
	}
	return canonical
}

// ParseExtendedMarket converts Extended market to canonical format
// e.g., "BTC-USD" -> "BTCUSD"
func ParseExtendedMarket(market string) string {
	return strings.ReplaceAll(market, "-", "")
}
