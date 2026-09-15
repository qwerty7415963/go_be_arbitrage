package exchange

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	binanceWSBaseURL = "wss://fstream.binance.com/ws/"
)

type BinanceWSAdapter struct {
	venueID      uuid.UUID
	client       *http.Client
	wsClients    map[string]*WSClient
	eventChans   map[string]chan *OrderBookEvent
	snapshotReqs map[string]chan struct{}
	mu           sync.RWMutex
}

func NewBinanceWSAdapter(venueID uuid.UUID) *BinanceWSAdapter {
	return &BinanceWSAdapter{
		venueID:      venueID,
		client:       &http.Client{Timeout: 10 * time.Second},
		wsClients:    make(map[string]*WSClient),
		eventChans:   make(map[string]chan *OrderBookEvent),
		snapshotReqs: make(map[string]chan struct{}),
	}
}

type BinanceDepthMessage struct {
	E string     `json:"e"`
	E2 int64     `json:"E"`
	S string     `json:"s"`
	U int64      `json:"U"`
	U2 int64     `json:"u"`
	B [][]string `json:"b"`
	A [][]string `json:"a"`
}

type BinanceDepthSnapshot struct {
	LastUpdateId int64      `json:"lastUpdateId"`
	E            int64      `json:"E"`
	T            int64      `json:"T"`
	Bids         [][]string `json:"bids"`
	Asks         [][]string `json:"asks"`
}

func (a *BinanceWSAdapter) SubscribeOrderBook(ctx context.Context, symbol string, depth int) (<-chan *OrderBookEvent, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if ch, ok := a.eventChans[symbol]; ok {
		return ch, nil
	}

	eventChan := make(chan *OrderBookEvent, 100)
	snapshotReady := make(chan struct{}, 1)
	a.eventChans[symbol] = eventChan
	a.snapshotReqs[symbol] = snapshotReady

	streamName := strings.ToLower(symbol) + "@depth@100ms"
	url := binanceWSBaseURL + streamName

	wsClient := NewWSClient(url, DefaultWSClientConfig())
	a.wsClients[symbol] = wsClient

	wsClient.SetCallbacks(
		func(data []byte) { a.handleDepthMessage(symbol, data, eventChan) },
		func() {
			snapshotReady <- struct{}{}
			go a.fetchAndSendSnapshot(ctx, symbol, eventChan, depth)
		},
		func() {
			go func() {
				if err := wsClient.Reconnect(ctx); err != nil {
					close(eventChan)
				}
			}()
		},
	)

	if err := wsClient.Connect(ctx); err != nil {
		return nil, fmt.Errorf("connect ws: %w", err)
	}

	return eventChan, nil
}

func (a *BinanceWSAdapter) handleDepthMessage(symbol string, data []byte, eventChan chan *OrderBookEvent) {
	var msg BinanceDepthMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return
	}
	if msg.E != "depthUpdate" {
		return
	}

	now := time.Now()

	if len(msg.B) > 0 {
		bids := make([]PriceLevel, 0, len(msg.B))
		for _, b := range msg.B {
			if len(b) >= 2 {
				bids = append(bids, PriceLevel{Price: b[0], Quantity: b[1]})
			}
		}
		select {
		case eventChan <- &OrderBookEvent{VenueSymbol: symbol, Bids: bids, Timestamp: now, FromSequence: msg.U, Sequence: msg.U2}:
		default:
		}
	}

	if len(msg.A) > 0 {
		asks := make([]PriceLevel, 0, len(msg.A))
		for _, a := range msg.A {
			if len(a) >= 2 {
				asks = append(asks, PriceLevel{Price: a[0], Quantity: a[1]})
			}
		}
		select {
		case eventChan <- &OrderBookEvent{VenueSymbol: symbol, Asks: asks, Timestamp: now, FromSequence: msg.U, Sequence: msg.U2}:
		default:
		}
	}
}

func (a *BinanceWSAdapter) fetchAndSendSnapshot(ctx context.Context, symbol string, eventChan chan *OrderBookEvent, depth int) {
	snapshot, err := a.fetchDepthSnapshot(ctx, symbol, depth)
	if err != nil {
		return
	}

	now := time.Now()

	bids := make([]PriceLevel, 0, len(snapshot.Bids))
	for _, b := range snapshot.Bids {
		if len(b) >= 2 {
			bids = append(bids, PriceLevel{Price: b[0], Quantity: b[1]})
		}
	}

	asks := make([]PriceLevel, 0, len(snapshot.Asks))
	for _, a := range snapshot.Asks {
		if len(a) >= 2 {
			asks = append(asks, PriceLevel{Price: a[0], Quantity: a[1]})
		}
	}

	event := &OrderBookEvent{
		VenueSymbol: symbol,
		Bids:        bids,
		Asks:        asks,
		Timestamp:   now,
		IsSnapshot:  true,
		Sequence:    snapshot.LastUpdateId,
	}

	select {
	case eventChan <- event:
	default:
	}
}

func (a *BinanceWSAdapter) fetchDepthSnapshot(ctx context.Context, symbol string, limit int) (*BinanceDepthSnapshot, error) {
	url := fmt.Sprintf("%s/fapi/v1/depth?symbol=%s&limit=%d", binanceBaseURL, symbol, limit)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var snapshot BinanceDepthSnapshot
	if err := json.Unmarshal(body, &snapshot); err != nil {
		return nil, err
	}

	return &snapshot, nil
}

func (a *BinanceWSAdapter) Disconnect(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	for symbol, wsClient := range a.wsClients {
		wsClient.Disconnect()
		delete(a.wsClients, symbol)
	}

	for symbol := range a.eventChans {
		delete(a.eventChans, symbol)
	}
	for symbol := range a.snapshotReqs {
		delete(a.snapshotReqs, symbol)
	}

	return nil
}

func (a *BinanceWSAdapter) IsConnected() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()

	for _, wsClient := range a.wsClients {
		if wsClient.IsConnected() {
			return true
		}
	}
	return false
}
