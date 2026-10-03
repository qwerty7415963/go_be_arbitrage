package hyperliquid

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// DefaultWSURL is the public Hyperliquid WebSocket endpoint (no auth).
const DefaultWSURL = "wss://api.hyperliquid.xyz/ws"

// DefaultMaxCoins caps trade subscriptions (BE-009: universe observed at 234;
// headroom without risking a subscription-limit breach).
const DefaultMaxCoins = 300

// WSTradeEvent is one buyer/seller pair from the public trades channel.
// Addresses are raw here; the harvest layer normalizes and skips malformed.
type WSTradeEvent struct {
	Coin   string
	Time   time.Time
	Buyer  string
	Seller string
}

type wsEnvelope struct {
	Channel string          `json:"channel"`
	Data    json.RawMessage `json:"data"`
}

type wsTrade struct {
	Coin  string   `json:"coin"`
	Time  int64    `json:"time"`
	Users []string `json:"users"`
}

// parseTradeFrame extracts trade events from one socket frame. handled=false
// for non-trades channels (subscription acks, pongs, heartbeats). Malformed
// frames and explicit error-channel payloads are errors (BE-031: surface,
// never silently ingest).
func parseTradeFrame(raw []byte) (events []WSTradeEvent, handled bool, err error) {
	var env wsEnvelope
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(&env); err != nil {
		return nil, false, fmt.Errorf("hyperliquid ws: bad frame: %w", err)
	}
	switch env.Channel {
	case "trades":
		handled = true
	case "error":
		return nil, false, fmt.Errorf("hyperliquid ws error channel: %s", string(env.Data))
	default:
		return nil, false, nil
	}
	var trades []wsTrade
	if err := json.Unmarshal(env.Data, &trades); err != nil {
		return nil, true, fmt.Errorf("hyperliquid ws: bad trades payload: %w", err)
	}
	for _, t := range trades {
		if len(t.Users) != 2 || t.Time <= 0 {
			continue
		}
		events = append(events, WSTradeEvent{
			Coin: t.Coin, Time: time.UnixMilli(t.Time).UTC(),
			Buyer: t.Users[0], Seller: t.Users[1],
		})
	}
	return events, true, nil
}

// TradeStream maintains public-trades subscriptions and streams buyer/seller
// pairs to onTrades. One instance serves one venue.
type TradeStream struct {
	wsURL    string
	maxCoins int
	onTrades func([]WSTradeEvent)
	logf     func(format string, args ...any)

	mu         sync.Mutex
	coins      []string
	subscribed map[string]bool
	sender     func(v any) error // set while connected; stubbed in unit tests

	frames     atomic.Int64
	events     atomic.Int64
	reconnects atomic.Int64
	subsSent   atomic.Int64
	overCap    atomic.Int64
}

func NewTradeStream(wsURL string, maxCoins int, onTrades func([]WSTradeEvent)) *TradeStream {
	if wsURL == "" {
		wsURL = DefaultWSURL
	}
	if maxCoins <= 0 {
		maxCoins = DefaultMaxCoins
	}
	return &TradeStream{
		wsURL: wsURL, maxCoins: maxCoins, onTrades: onTrades,
		subscribed: map[string]bool{}, logf: log.Printf,
	}
}

// TradeStreamStats is a point-in-time snapshot (observability).
type TradeStreamStats struct {
	Frames     int64
	Events     int64
	Reconnects int64
	SubsSent   int64
	OverCap    int64
	Coins      int
	Subscribed int
}

func (s *TradeStream) Stats() TradeStreamStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return TradeStreamStats{
		Frames: s.frames.Load(), Events: s.events.Load(),
		Reconnects: s.reconnects.Load(), SubsSent: s.subsSent.Load(),
		OverCap: s.overCap.Load(), Coins: len(s.coins), Subscribed: len(s.subscribed),
	}
}

// SetCoins replaces the subscription universe (BE-009: over-cap refused and
// counted, never partially applied).
func (s *TradeStream) SetCoins(coins []string) error {
	if len(coins) > s.maxCoins {
		s.overCap.Add(1)
		return fmt.Errorf("hyperliquid ws: %d coins exceed cap %d", len(coins), s.maxCoins)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.coins = append([]string(nil), coins...)
	return nil
}

// RefreshCoins subscribes coins not yet covered (BE-008: dynamic universe,
// no restart). Returns the newly subscribed set.
func (s *TradeStream) RefreshCoins(coins []string) ([]string, error) {
	if len(coins) > s.maxCoins {
		s.overCap.Add(1)
		return nil, fmt.Errorf("hyperliquid ws: %d coins exceed cap %d", len(coins), s.maxCoins)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var added []string
	for _, c := range coins {
		if !s.subscribed[c] {
			added = append(added, c)
		}
	}
	s.coins = append([]string(nil), coins...)
	for _, c := range added {
		if err := s.send(map[string]any{
			"method":       "subscribe",
			"subscription": map[string]any{"type": "trades", "coin": c},
		}); err != nil {
			return added, err
		}
		s.subscribed[c] = true
	}
	return added, nil
}

func (s *TradeStream) send(v any) error {
	if s.sender == nil {
		return nil // not connected: covered on (re)connect
	}
	if err := s.sender(v); err != nil {
		return err
	}
	s.subsSent.Add(1)
	return nil
}

// subscribeAll (re)sends every coin subscription (connect + reconnect path,
// BE-007).
func (s *TradeStream) subscribeAll() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.coins {
		if err := s.send(map[string]any{
			"method":       "subscribe",
			"subscription": map[string]any{"type": "trades", "coin": c},
		}); err != nil {
			return err
		}
		s.subscribed[c] = true
	}
	return nil
}

// Run connects and streams until ctx ends, with backoff reconnects that
// restore every subscription (BE-007).
func (s *TradeStream) Run(ctx context.Context) error {
	backoff := time.Second
	for {
		if err := s.connect(ctx); err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
			}
			s.logf("hyperliquid ws: connect failed (%v), retry in %s", err, backoff)
			if !sleepCtx(ctx, backoff) {
				return nil
			}
			backoff *= 2
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
			continue
		}
		backoff = time.Second
	}
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func (s *TradeStream) connect(ctx context.Context) error {
	dialer := websocket.Dialer{HandshakeTimeout: 15 * time.Second}
	conn, _, err := dialer.DialContext(ctx, s.wsURL, nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	s.mu.Lock()
	s.sender = conn.WriteJSON
	s.subscribed = map[string]bool{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.sender = nil
		s.mu.Unlock()
	}()

	if err := s.subscribeAll(); err != nil {
		return err
	}

	conn.SetReadLimit(8 << 20)
	_ = conn.SetReadDeadline(time.Now().Add(90 * time.Second))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(90 * time.Second))
	})
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		_, raw, err := conn.ReadMessage()
		if err != nil {
			s.reconnects.Add(1)
			return err
		}
		_ = conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		s.frames.Add(1)
		events, handled, err := parseTradeFrame(raw)
		if err != nil {
			s.logf("hyperliquid ws: frame error: %v", err)
			continue
		}
		if !handled || len(events) == 0 {
			continue
		}
		s.events.Add(int64(len(events)))
		if s.onTrades != nil {
			s.onTrades(events)
		}
	}
}

// FetchPerpCoins returns the perp universe coin names (subscription source).
func (c *Client) FetchPerpCoins(ctx context.Context) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	body, _ := json.Marshal(map[string]string{"type": "meta"})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/info", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("hyperliquid meta: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		c.noteStatus(resp.StatusCode)
		return nil, fmt.Errorf("hyperliquid meta: status %d", resp.StatusCode)
	}
	c.noteStatus(resp.StatusCode)
	var meta struct {
		Universe []struct {
			Name string `json:"name"`
		} `json:"universe"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		return nil, fmt.Errorf("hyperliquid meta decode: %w", err)
	}
	if len(meta.Universe) == 0 {
		return nil, fmt.Errorf("hyperliquid meta: empty universe (schema drift?)")
	}
	out := make([]string, 0, len(meta.Universe))
	for _, u := range meta.Universe {
		if u.Name != "" {
			out = append(out, u.Name)
		}
	}
	return out, nil
}
