package trader

import (
	"testing"

	"github.com/qwerty7415963/go_be_arbitrage/internal/hyperliquid"
)

func f64p(v float64) *float64 { return &v }

func TestAttachTPSL_EmptyPositions(t *testing.T) {
	// TPSL-01
	got := attachTPSL(nil, nil)
	if got == nil || len(got) != 0 {
		t.Fatalf("want empty non-nil slice, got %v", got)
	}
	got = attachTPSL([]Position{}, []hyperliquid.OpenOrder{})
	if got == nil || len(got) != 0 {
		t.Fatalf("want empty non-nil slice, got %v", got)
	}
}

func TestAttachTPSL_NoOrders(t *testing.T) {
	// TPSL-02
	positions := []Position{{Coin: "BTC", Side: "LONG", MarkPrice: f64p(100)}}
	got := attachTPSL(positions, nil)
	if len(got) != 1 {
		t.Fatalf("len: %d", len(got))
	}
	if got[0].TpPxs == nil || len(got[0].TpPxs) != 0 {
		t.Errorf("TpPxs must be []: %v", got[0].TpPxs)
	}
	if got[0].SlPxs == nil || len(got[0].SlPxs) != 0 {
		t.Errorf("SlPxs must be []: %v", got[0].SlPxs)
	}
}

func TestAttachTPSL_BasicTPSL(t *testing.T) {
	// TPSL-03, TPSL-04
	orders := []hyperliquid.OpenOrder{
		{Coin: "BTC", Side: "A", ReduceOnly: true, OrderType: "Take Profit Market", TriggerPx: "110"},
		{Coin: "BTC", Side: "A", ReduceOnly: true, OrderType: "Stop Market", TriggerPx: "90"},
		{Coin: "BTC", Side: "B", ReduceOnly: true, OrderType: "Take Profit Market", TriggerPx: "200"}, // not closing
	}
	positions := []Position{{Coin: "BTC", Side: "LONG", MarkPrice: f64p(100)}}
	got := attachTPSL(positions, orders)
	if len(got[0].TpPxs) != 1 || got[0].TpPxs[0] != 110 {
		t.Errorf("TpPxs: %v", got[0].TpPxs)
	}
	if len(got[0].SlPxs) != 1 || got[0].SlPxs[0] != 90 {
		t.Errorf("SlPxs: %v", got[0].SlPxs)
	}
}

func TestAttachTPSL_ChildrenFlatten(t *testing.T) {
	// TPSL-10
	parent := hyperliquid.OpenOrder{
		Coin: "BTC", Side: "A", ReduceOnly: true, OrderType: "Take Profit Market",
		Children: []hyperliquid.OpenOrder{
			{TriggerPx: "115"}, // inherits
		},
	}
	positions := []Position{{Coin: "BTC", Side: "LONG", MarkPrice: f64p(100)}}
	got := attachTPSL(positions, []hyperliquid.OpenOrder{parent})
	if len(got[0].TpPxs) != 1 || got[0].TpPxs[0] != 115 {
		t.Fatalf("want child tp px 115, got %v", got[0].TpPxs)
	}
}

func TestAttachTPSL_CoinSuffix(t *testing.T) {
	// TPSL-11
	orders := []hyperliquid.OpenOrder{
		{Coin: "xyz:BTC", Side: "A", ReduceOnly: true, OrderType: "Stop Market", TriggerPx: "95"},
	}
	positions := []Position{{Coin: "BTC", Side: "LONG", MarkPrice: f64p(100)}}
	got := attachTPSL(positions, orders)
	if len(got[0].SlPxs) != 1 || got[0].SlPxs[0] != 95 {
		t.Errorf("SlPxs: %v", got[0].SlPxs)
	}
}

func TestAttachTPSL_ShortPositionClosingSide(t *testing.T) {
	// TPSL-12
	orders := []hyperliquid.OpenOrder{
		{Coin: "ETH", Side: "B", ReduceOnly: true, OrderType: "Take Profit Market", TriggerPx: "1800"},
		{Coin: "ETH", Side: "B", ReduceOnly: true, OrderType: "Stop Market", TriggerPx: "2200"},
	}
	positions := []Position{{Coin: "ETH", Side: "SHORT", MarkPrice: f64p(2000)}}
	got := attachTPSL(positions, orders)
	if len(got[0].TpPxs) != 1 || got[0].TpPxs[0] != 1800 {
		t.Errorf("TpPxs: %v", got[0].TpPxs)
	}
	if len(got[0].SlPxs) != 1 || got[0].SlPxs[0] != 2200 {
		t.Errorf("SlPxs: %v", got[0].SlPxs)
	}
}

func TestAttachTPSL_SortByMarkAbs(t *testing.T) {
	// TPSL-13
	orders := []hyperliquid.OpenOrder{
		{Coin: "BTC", Side: "A", ReduceOnly: true, OrderType: "Take Profit Market", TriggerPx: "130"},
		{Coin: "BTC", Side: "A", ReduceOnly: true, OrderType: "Take Profit Market", TriggerPx: "110"},
		{Coin: "BTC", Side: "A", ReduceOnly: true, OrderType: "Take Profit Market", TriggerPx: "120"},
	}
	positions := []Position{{Coin: "BTC", Side: "LONG", MarkPrice: f64p(100)}}
	got := attachTPSL(positions, orders)
	want := []float64{110, 120, 130}
	for i := range want {
		if got[0].TpPxs[i] != want[i] {
			t.Errorf("got %v want %v", got[0].TpPxs, want)
		}
	}
}

func TestAttachTPSL_MarkNilSortsPriceAsc(t *testing.T) {
	// TPSL-14
	orders := []hyperliquid.OpenOrder{
		{Coin: "BTC", Side: "A", ReduceOnly: true, OrderType: "Stop Market", TriggerPx: "95"},
		{Coin: "BTC", Side: "A", ReduceOnly: true, OrderType: "Stop Market", TriggerPx: "85"},
	}
	positions := []Position{{Coin: "BTC", Side: "LONG", MarkPrice: nil}}
	got := attachTPSL(positions, orders)
	if got[0].SlPxs[0] != 85 || got[0].SlPxs[1] != 95 {
		t.Errorf("got %v", got[0].SlPxs)
	}
}

func TestAttachTPSL_DedupeCanonical(t *testing.T) {
	// TPSL-15
	orders := []hyperliquid.OpenOrder{
		{Coin: "BTC", Side: "A", ReduceOnly: true, OrderType: "Take Profit Market", TriggerPx: "110"},
		{Coin: "BTC", Side: "A", ReduceOnly: true, OrderType: "Take Profit Market", TriggerPx: "110.0"},
	}
	positions := []Position{{Coin: "BTC", Side: "LONG", MarkPrice: f64p(100)}}
	got := attachTPSL(positions, orders)
	if len(got[0].TpPxs) != 1 {
		t.Errorf("dedupe failed: %v", got[0].TpPxs)
	}
}

func TestAttachTPSL_TieBreakPriceAsc(t *testing.T) {
	// TPSL-16
	orders := []hyperliquid.OpenOrder{
		{Coin: "BTC", Side: "A", ReduceOnly: true, OrderType: "Take Profit Market", TriggerPx: "120"},
		{Coin: "BTC", Side: "A", ReduceOnly: true, OrderType: "Take Profit Market", TriggerPx: "80"},
	}
	positions := []Position{{Coin: "BTC", Side: "LONG", MarkPrice: f64p(100)}}
	got := attachTPSL(positions, orders)
	// |120-100|=20, |80-100|=20 tie -> price asc: 80 then 120
	if got[0].TpPxs[0] != 80 || got[0].TpPxs[1] != 120 {
		t.Errorf("got %v", got[0].TpPxs)
	}
}

func TestAttachTPSL_ReduceOnlyFalseExcluded(t *testing.T) {
	// TPSL-17
	orders := []hyperliquid.OpenOrder{
		{Coin: "BTC", Side: "A", ReduceOnly: false, OrderType: "Take Profit Market", TriggerPx: "110"},
	}
	positions := []Position{{Coin: "BTC", Side: "LONG", MarkPrice: f64p(100)}}
	got := attachTPSL(positions, orders)
	if len(got[0].TpPxs) != 0 {
		t.Errorf("got %v", got[0].TpPxs)
	}
}

func TestAttachTPSL_TriggerPxZeroExcluded(t *testing.T) {
	// TPSL-18
	orders := []hyperliquid.OpenOrder{
		{Coin: "BTC", Side: "A", ReduceOnly: true, OrderType: "Take Profit Market", TriggerPx: "0"},
	}
	positions := []Position{{Coin: "BTC", Side: "LONG", MarkPrice: f64p(100)}}
	got := attachTPSL(positions, orders)
	if len(got[0].TpPxs) != 0 {
		t.Errorf("got %v", got[0].TpPxs)
	}
}

func TestAttachTPSL_InvalidTriggerExcluded(t *testing.T) {
	// TPSL-19
	orders := []hyperliquid.OpenOrder{
		{Coin: "BTC", Side: "A", ReduceOnly: true, OrderType: "Take Profit Market", TriggerPx: "abc"},
	}
	positions := []Position{{Coin: "BTC", Side: "LONG", MarkPrice: f64p(100)}}
	got := attachTPSL(positions, orders)
	if len(got[0].TpPxs) != 0 {
		t.Errorf("got %v", got[0].TpPxs)
	}
}

func TestAttachTPSL_DoesNotMutateInput(t *testing.T) {
	// TPSL-21
	positions := []Position{{Coin: "BTC", Side: "LONG", MarkPrice: f64p(100)}}
	_ = attachTPSL(positions, nil)
	if len(positions[0].TpPxs) != 0 {
		t.Errorf("input mutated")
	}
}

func TestAttachTPSL_NonNilArraysEmpty(t *testing.T) {
	// TPSL-26
	positions := []Position{{Coin: "BTC", Side: "LONG", MarkPrice: f64p(100)}}
	got := attachTPSL(positions, []hyperliquid.OpenOrder{})
	if got[0].TpPxs == nil || got[0].SlPxs == nil {
		t.Errorf("nil arrays")
	}
}

func TestAttachTPSL_SortHelper(t *testing.T) {
	// TPSL-31
	mark := 100.0
	prices := []float64{130, 110, 120}
	out := sortTPSL(prices, &mark)
	if out[0] != 110 || out[1] != 120 || out[2] != 130 {
		t.Errorf("got %v", out)
	}
}

func TestFlattenOrders_InheritsFields(t *testing.T) {
	parent := hyperliquid.OpenOrder{
		Coin: "BTC", Side: "A", ReduceOnly: true, OrderType: "Take Profit Market", TriggerPx: "110",
		Children: []hyperliquid.OpenOrder{{}},
	}
	got := flattenOrders([]hyperliquid.OpenOrder{parent})
	if len(got) != 2 {
		t.Fatalf("len %d", len(got))
	}
	if got[1].Coin != "BTC" || got[1].Side != "A" || !got[1].ReduceOnly || got[1].TriggerPx != "110" {
		t.Errorf("inherit failed: %+v", got[1])
	}
}

func TestNormCoin_Suffix(t *testing.T) {
	if normCoin("xyz:BTC") != "BTC" {
		t.Error("suffix")
	}
	if normCoin("btc") != "BTC" {
		t.Error("upper")
	}
}

func TestParseTrigger_Basic(t *testing.T) {
	if v, ok := parseTrigger("12.5"); !ok || v != 12.5 {
		t.Errorf("got %v %v", v, ok)
	}
	if _, ok := parseTrigger("abc"); ok {
		t.Error("want false")
	}
}

func TestIsTP_IsSL(t *testing.T) {
	o := hyperliquid.OpenOrder{OrderType: "Take Profit Market"}
	if !isTP(o) {
		t.Error("isTP")
	}
	o2 := hyperliquid.OpenOrder{OrderType: "Stop Market"}
	if !isSL(o2) {
		t.Error("isSL")
	}
}

func TestDedupeFloats_Basic(t *testing.T) {
	got := dedupeFloats([]float64{1, 1, 2})
	if len(got) != 2 {
		t.Errorf("got %v", got)
	}
}
