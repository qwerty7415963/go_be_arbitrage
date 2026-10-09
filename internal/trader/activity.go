package trader

import (
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/qwerty7415963/go_be_arbitrage/internal/hyperliquid"
)

// ActivityFill is one realtime fill-level event (no PnL).
type ActivityFill struct {
	Coin  string    `json:"coin"`
	Side  string    `json:"side"` // BUY | SELL (relative to watched wallet)
	Size  float64   `json:"size"`
	Price float64   `json:"price"`
	Time  time.Time `json:"time"`
	Tid   int64     `json:"tid"`
}

// ActivityClient is one WS conn watching one wallet. The hub only moves
// opaque JSON bytes; the WS layer owns the conn and pumps.
type ActivityClient struct {
	wallet string
	send   chan []byte
}

// CompletedTradeRow is one closed-trade row (live universe or durable
// trader_trades). EntryPrice/ExitPrice/Size are NULL for pre-000030 rows
// whose fills aged out of trader_fill_buffer (contract WALLET-TABS v1 §3/§9).
// Funding is the signed informational attribution (negative = paid,
// LIVE-CONTRACT v1.2 §1.2): sum of userFunding payments with
// openTime ≤ time ≤ closeTime per coin. Never decides win/loss; NetPnl stays
// pnl − fees. DB rows carry 0 (column does not exist); live rows carry the
// attributed sum.
type CompletedTradeRow struct {
	Market     string
	Side       string // LONG | SHORT
	OpenedAt   time.Time
	ClosedAt   time.Time
	Volume     float64
	PnL        float64
	Fees       float64
	Funding    float64
	Fills      int
	EntryPrice *float64
	ExitPrice  *float64
	Size       *float64
}

// NetPnl computes net = pnl - fees server-side.
func (r CompletedTradeRow) NetPnl() float64 { return r.PnL - r.Fees }

// ActivityHub holds live subscribers keyed by normalized wallet address.
// Mirrors internal/orderbook.WsHub but keyed per wallet.
type ActivityHub struct {
	mu   sync.RWMutex
	subs map[string]map[*ActivityClient]bool
}

func NewActivityHub() *ActivityHub {
	return &ActivityHub{subs: map[string]map[*ActivityClient]bool{}}
}

// Subscribe registers one client for wallet (lowercased; callers should pass
// NormalizeAddress output). Buffer 64; Publish drops on full (non-blocking).
func (h *ActivityHub) Subscribe(wallet string, bufSize ...int) *ActivityClient {
	wallet = strings.ToLower(strings.TrimSpace(wallet))
	n := 64
	if len(bufSize) > 0 && bufSize[0] > 0 {
		n = bufSize[0]
	}
	c := &ActivityClient{wallet: wallet, send: make(chan []byte, n)}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.subs[wallet] == nil {
		h.subs[wallet] = map[*ActivityClient]bool{}
	}
	h.subs[wallet][c] = true
	return c
}

// Unsubscribe removes the client and closes its send chan.
func (h *ActivityHub) Unsubscribe(c *ActivityClient) {
	if c == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	set := h.subs[c.wallet]
	if set == nil || !set[c] {
		return
	}
	delete(set, c)
	close(c.send)
	if len(set) == 0 {
		delete(h.subs, c.wallet)
	}
}

// WatchSet returns currently-watched (subscribed) addresses.
func (h *ActivityHub) WatchSet() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]string, 0, len(h.subs))
	for w, set := range h.subs {
		if len(set) > 0 {
			out = append(out, w)
		}
	}
	return out
}

// IsWatched reports whether wallet has at least one subscriber.
func (h *ActivityHub) IsWatched(wallet string) bool {
	wallet = strings.ToLower(strings.TrimSpace(wallet))
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subs[wallet]) > 0
}

// SubscriberCount returns the subscriber count for wallet (tests).
func (h *ActivityHub) SubscriberCount(wallet string) int {
	wallet = strings.ToLower(strings.TrimSpace(wallet))
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subs[wallet])
}

// Publish forwards fill to wallet subscribers as {type:"activity",data:fill}.
// Non-blocking: drops on full client buffers.
func (h *ActivityHub) Publish(wallet string, fill ActivityFill) {
	wallet = strings.ToLower(strings.TrimSpace(wallet))
	msg, err := json.Marshal(map[string]any{"type": "activity", "data": fill})
	if err != nil {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.subs[wallet] {
		select {
		case c.send <- msg:
		default:
		}
	}
}

// WSActivityService filters venue trade fills against the watched set and
// publishes realtime activity. One instance per venue.
type WSActivityService struct {
	hub *ActivityHub
}

func NewWSActivityService(hub *ActivityHub) *WSActivityService {
	return &WSActivityService{hub: hub}
}

// Submit publishes fills involving watched wallets. Side is relative to each
// wallet (buyer => BUY, seller => SELL); self-trades (buyer==seller) publish
// once as BUY. Malformed addresses are skipped.
func (s *WSActivityService) Submit(fills []hyperliquid.WSTradeFill) {
	if s == nil || s.hub == nil {
		return
	}
	for _, f := range fills {
		buyer, errB := NormalizeAddress(f.Buyer)
		seller, errS := NormalizeAddress(f.Seller)
		if errB == nil && s.hub.IsWatched(buyer) {
			s.hub.Publish(buyer, ActivityFill{
				Coin: f.Coin, Side: "BUY",
				Size: f.Sz, Price: f.Px, Time: f.Time, Tid: f.Tid,
			})
		}
		if errS == nil && (errB != nil || seller != buyer) && s.hub.IsWatched(seller) {
			s.hub.Publish(seller, ActivityFill{
				Coin: f.Coin, Side: "SELL",
				Size: f.Sz, Price: f.Px, Time: f.Time, Tid: f.Tid,
			})
		}
	}
}
