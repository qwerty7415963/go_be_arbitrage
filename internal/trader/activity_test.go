package trader

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/qwerty7415963/go_be_arbitrage/internal/hyperliquid"
)

func watchedHub() (*ActivityHub, string, string) {
	hub := NewActivityHub()
	watched := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	other := "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	hub.Subscribe(watched)
	return hub, watched, other
}

func testFill(buyer, seller string) hyperliquid.WSTradeFill {
	return hyperliquid.WSTradeFill{
		Coin: "BTC", Px: 60000, Sz: 0.1, Side: "B",
		Time: time.Now().UTC(), Tid: 123,
		Buyer: buyer, Seller: seller,
	}
}

// Submit publishes only to watched wallets with correct BUY/SELL attribution.
func TestWSActivityService_SubmitFiltering(t *testing.T) {
	hub, watched, other := watchedHub()
	otherSub := hub.Subscribe(other)
	svc := NewWSActivityService(hub)
	unwatchedBuyer := "0xcccccccccccccccccccccccccccccccccccccccc"
	unwatchedSeller := "0xdddddddddddddddddddddddddddddddddddddddd"

	// watched is buyer => BUY; other is seller but unwatched? make other watched seller case separately.
	svc.Submit([]hyperliquid.WSTradeFill{testFill(watched, unwatchedSeller)})
	svc.Submit([]hyperliquid.WSTradeFill{testFill(unwatchedBuyer, watched)})
	svc.Submit([]hyperliquid.WSTradeFill{testFill(unwatchedBuyer, unwatchedSeller)})
	if len(otherSub.send) != 0 {
		t.Errorf("unrelated watched client must receive 0 messages, got %d", len(otherSub.send))
	}

	subs := hub.subs[watched]
	if len(subs) != 1 {
		t.Fatalf("hub subs: %d", len(subs))
	}
	var c *ActivityClient
	for k := range subs {
		c = k
	}
	if len(c.send) != 2 {
		t.Fatalf("want 2 publishes (BUY + SELL), got %d", len(c.send))
	}
	first := <-c.send
	var msg struct {
		Type string       `json:"type"`
		Data ActivityFill `json:"data"`
	}
	if err := json.Unmarshal(first, &msg); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if msg.Type != "activity" || msg.Data.Side != "BUY" || msg.Data.Coin != "BTC" {
		t.Errorf("first (buyer=>BUY): %+v", msg)
	}
	second := <-c.send
	if err := json.Unmarshal(second, &msg); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if msg.Data.Side != "SELL" {
		t.Errorf("second (seller=>SELL): %+v", msg)
	}
}

// Taker-buyer vs taker-seller: attribution follows wallet role, not taker side.
func TestWSActivityService_SideAttribution(t *testing.T) {
	hub := NewActivityHub()
	buyer := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	seller := "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	hub.Subscribe(buyer)
	hub.Subscribe(seller)
	svc := NewWSActivityService(hub)

	// Taker sold (side A) but buyer wallet still BUY, seller wallet still SELL.
	f := testFill(buyer, seller)
	f.Side = "A"
	svc.Submit([]hyperliquid.WSTradeFill{f})

	for addr, want := range map[string]string{buyer: "BUY", seller: "SELL"} {
		var c *ActivityClient
		for k := range hub.subs[addr] {
			c = k
		}
		if len(c.send) != 1 {
			t.Fatalf("%s: want 1 msg, got %d", addr, len(c.send))
		}
		raw := <-c.send
		var msg struct {
			Data ActivityFill `json:"data"`
		}
		if err := json.Unmarshal(raw, &msg); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if msg.Data.Side != want {
			t.Errorf("%s: want %s, got %s", addr, want, msg.Data.Side)
		}
	}
}

// Non-blocking publish: full buffer drops instead of blocking.
func TestActivityHub_PublishNonBlocking(t *testing.T) {
	hub := NewActivityHub()
	wallet := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	c := hub.Subscribe(wallet, 1)
	fill := ActivityFill{Coin: "BTC", Side: "BUY", Size: 0.1, Price: 1, Time: time.Now().UTC(), Tid: 1}
	hub.Publish(wallet, fill) // fills buffer (1)
	hub.Publish(wallet, fill) // must drop, not block
	hub.Publish(wallet, fill) // must drop, not block
	if len(c.send) != 1 {
		t.Errorf("buffer must hold exactly 1 (rest dropped), got %d", len(c.send))
	}
	if got := hub.WatchSet(); len(got) != 1 || got[0] != wallet {
		t.Errorf("watchset: %v", got)
	}
	hub.Unsubscribe(c)
	if hub.SubscriberCount(wallet) != 0 || len(hub.WatchSet()) != 0 {
		t.Error("unsubscribe must remove the wallet")
	}
}

// Self-trade publishes once.
func TestWSActivityService_SelfTrade(t *testing.T) {
	hub := NewActivityHub()
	addr := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	hub.Subscribe(addr)
	svc := NewWSActivityService(hub)
	svc.Submit([]hyperliquid.WSTradeFill{testFill(addr, addr)})
	var c *ActivityClient
	for k := range hub.subs[addr] {
		c = k
	}
	if len(c.send) != 1 {
		t.Errorf("self-trade must publish once, got %d", len(c.send))
	}
}
