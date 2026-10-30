package notify

import (
	"testing"

	"github.com/qwerty7415963/go_be_arbitrage/internal/trader"
)

func TestMapper_LimitPlaced(t *testing.T) {
	ev := trader.WalletEvent{Type: trader.EvtOrderCreated, Wallet: "0xabc", Data: trader.OrderCreatedPayload{
		OrderDTO: trader.OrderDTO{Coin: "BTC", Side: "BUY", LimitPx: 100, Size: 1.5, Oid: 1, OrderType: "Limit", ReduceOnly: false},
		Status:   "open",
	}}
	it, ok := MapWalletEvent(ev)
	if !ok || it.Kind != "limit_placed" {
		t.Fatalf("got %+v", it)
	}
	if it.DedupKey != "user:00000000-0000-0000-0000-000000000000:order:1:created" {
		t.Fatalf("dedup %s", it.DedupKey)
	}
}

func TestMapper_CloseLimitPlaced(t *testing.T) {
	ev := trader.WalletEvent{Type: trader.EvtOrderCreated, Wallet: "0xabc", Data: trader.OrderCreatedPayload{
		OrderDTO: trader.OrderDTO{Coin: "BTC", Side: "SELL", LimitPx: 100, Size: 1, Oid: 2, OrderType: "Limit", ReduceOnly: true},
		Status:   "open",
	}}
	it, ok := MapWalletEvent(ev)
	if !ok || it.Kind != "close_limit_placed" {
		t.Fatalf("got %+v", it)
	}
}

func TestMapper_OrderCanceled(t *testing.T) {
	st := "canceled"
	ev := trader.WalletEvent{Type: trader.EvtOrderUpdated, Wallet: "0xabc", Data: trader.OrderDTO{Coin: "BTC", Side: "BUY", LimitPx: 100, Size: 1, Oid: 3, OrderStatus: &st}}
	it, ok := MapWalletEvent(ev)
	if !ok || it.Kind != "order_canceled" {
		t.Fatalf("got %+v", it)
	}
	if it.DedupKey != "user:00000000-0000-0000-0000-000000000000:order:3:canceled" {
		t.Fatalf("dedup %s", it.DedupKey)
	}
}

func TestMapper_PositionOpened(t *testing.T) {
	ev := trader.WalletEvent{Type: trader.EvtFillCreated, Wallet: "0xabc", Data: trader.FillDTO{Coin: "ETH", Side: "BUY", Dir: "Open Long", Size: 2, Price: 50, Tid: 10}}
	it, ok := MapWalletEvent(ev)
	if !ok || it.Kind != "position_opened" {
		t.Fatalf("got %+v", it)
	}
	if it.DedupKey != "user:00000000-0000-0000-0000-000000000000:fill:10" {
		t.Fatalf("dedup %s", it.DedupKey)
	}
}

func TestMapper_PositionClosedTP(t *testing.T) {
	ev := trader.WalletEvent{Type: trader.EvtFillCreated, Wallet: "0xabc", Data: trader.FillDTO{Coin: "ETH", Side: "SELL", Dir: "Close Long", Size: 2, Price: 55, ClosedPnl: 10, Tid: 11}}
	it, ok := MapWalletEvent(ev)
	if !ok || it.Kind != "position_closed" {
		t.Fatalf("got %+v", it)
	}
}

func TestMapper_PositionClosedSL(t *testing.T) {
	ev := trader.WalletEvent{Type: trader.EvtFillCreated, Wallet: "0xabc", Data: trader.FillDTO{Coin: "ETH", Side: "SELL", Dir: "Close Long", Size: 2, Price: 45, ClosedPnl: -5, Tid: 12}}
	it, ok := MapWalletEvent(ev)
	if !ok || it.Kind != "position_closed" {
		t.Fatalf("got %+v", it)
	}
}

func TestMapper_TPSLClassification(t *testing.T) {
	cases := []struct {
		side, cond string
		reduce     bool
		want       string
	}{
		{"SELL", "above", true, "tp_placed"},
		{"SELL", "below", true, "sl_placed"},
		{"BUY", "below", true, "tp_placed"},
		{"BUY", "above", true, "sl_placed"},
	}
	for _, c := range cases {
		px := 105.0
		ev := trader.WalletEvent{Type: trader.EvtOrderCreated, Wallet: "0xabc", Data: trader.OrderCreatedPayload{
			OrderDTO: trader.OrderDTO{Coin: "BTC", Side: c.side, LimitPx: 100, Size: 1, Oid: 20, OrderType: "Trigger", ReduceOnly: c.reduce, TriggerCondition: c.cond, TriggerPx: &px},
			Status:   "open",
		}}
		it, ok := MapWalletEvent(ev)
		if !ok || it.Kind != c.want {
			t.Fatalf("side=%s cond=%s got %+v want %s", c.side, c.cond, it, c.want)
		}
	}
}
