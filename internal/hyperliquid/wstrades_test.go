package hyperliquid

import (
	"strings"
	"testing"
)

// WS-U-01: trades frames yield buyer/seller pairs; everything else is
// ignored or a loud error, never silently ingested.
func TestParseTradeFrame(t *testing.T) {
	body := `{"channel":"trades","data":[
		{"coin":"BTC","side":"A","px":"1","sz":"1","time":1727745600000,"hash":"h","tid":1,
		 "users":["0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"]},
		{"coin":"ETH","side":"B","px":"1","sz":"1","time":1727745660000,"hash":"h","tid":2,
		 "users":["0xcccccccccccccccccccccccccccccccccccccccc"]},
		{"coin":"SOL","side":"B","px":"1","sz":"1","time":0,"hash":"h","tid":3,
		 "users":["0xdddddddddddddddddddddddddddddddddddddddd","0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"]}
	]}`
	events, handled, err := parseTradeFrame([]byte(body))
	if err != nil || !handled {
		t.Fatalf("trades frame: %v %v", err, handled)
	}
	if len(events) != 1 {
		t.Fatalf("want 1 valid event (bad users/time skipped), got %d", len(events))
	}
	if events[0].Buyer != "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" ||
		events[0].Seller != "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" ||
		events[0].Coin != "BTC" {
		t.Errorf("event: %+v", events[0])
	}

	for name, frame := range map[string]string{
		"ack":     `{"channel":"subscriptionResponse","data":{}}`,
		"pong":    `{"channel":"pong","data":{}}`,
		"unknown": `{"channel":"somethingNew","data":[]}`,
	} {
		_, handled, err := parseTradeFrame([]byte(frame))
		if err != nil || handled {
			t.Errorf("%s: want ignored, got handled=%v err=%v", name, handled, err)
		}
	}

	for name, frame := range map[string]string{
		"garbage":       `not json`,
		"bad payload":   `{"channel":"trades","data":"oops"}`,
		"error channel": `{"channel":"error","data":"bad subscription"}`,
	} {
		if _, _, err := parseTradeFrame([]byte(frame)); err == nil {
			t.Errorf("%s: want loud error", name)
		}
	}
}

// WS-U-03: subscription cap refuses over-cap sets without partial apply.
func TestTradeStream_CapGuard(t *testing.T) {
	s := NewTradeStream("", 2, nil)
	if err := s.SetCoins([]string{"BTC", "ETH", "SOL"}); err == nil {
		t.Fatal("over-cap set must fail")
	}
	if got := s.Stats(); got.OverCap != 1 || got.Coins != 0 {
		t.Errorf("cap stats: %+v", got)
	}
	if err := s.SetCoins([]string{"BTC", "ETH"}); err != nil {
		t.Fatalf("at-cap set must pass: %v", err)
	}
}

// WS-U-04: (re)subscribe sends one frame per coin, no duplicates.
func TestTradeStream_SubscribeAll(t *testing.T) {
	var sent []string
	s := NewTradeStream("", 10, nil)
	s.sender = func(v any) error {
		m := v.(map[string]any)
		sent = append(sent, m["subscription"].(map[string]any)["coin"].(string))
		return nil
	}
	if err := s.SetCoins([]string{"BTC", "ETH"}); err != nil {
		t.Fatalf("coins: %v", err)
	}
	if err := s.subscribeAll(); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if err := s.subscribeAll(); err != nil {
		t.Fatalf("resubscribe: %v", err)
	}
	if len(sent) != 4 || strings.Join(sent, ",") != "BTC,ETH,BTC,ETH" {
		t.Errorf("frames: %v", sent)
	}
	if got := s.Stats(); got.SubsSent != 4 || got.Subscribed != 2 {
		t.Errorf("stats: %+v", got)
	}
}

// A2: fill-level parse — px/sz/side/tid/hash with skip-malformed semantics.
// Verified live field names (docs): coin,side,px,sz,hash,time,tid,users=[buyer,seller].
func TestParseTradeFillFrame(t *testing.T) {
	body := `{"channel":"trades","data":[
		{"coin":"BTC","side":"B","px":"60000.5","sz":"0.1","time":1727745600000,"hash":"0xabc","tid":12345,
		 "users":["0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"]},
		{"coin":"eth","side":"A","px":"3000","sz":"1","time":1727745660000,"hash":"0xdef","tid":12346,
		 "users":["0xcccccccccccccccccccccccccccccccccccccccc","0xdddddddddddddddddddddddddddddddddddddddd"]},
		{"coin":"SOL","side":"B","px":"bad","sz":"1","time":1727745660000,"hash":"h","tid":3,
		 "users":["0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee","0xffffffffffffffffffffffffffffffffffffffff"]},
		{"coin":"ARB","side":"X","px":"1","sz":"1","time":1727745660000,"hash":"h","tid":4,
		 "users":["0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee","0xffffffffffffffffffffffffffffffffffffffff"]},
		{"coin":"OP","side":"B","px":"1","sz":"0","time":1727745660000,"hash":"h","tid":5,
		 "users":["0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee","0xffffffffffffffffffffffffffffffffffffffff"]}
	]}`
	fills, handled, err := parseTradeFillFrame([]byte(body))
	if err != nil || !handled {
		t.Fatalf("fills frame: %v %v", err, handled)
	}
	if len(fills) != 2 {
		t.Fatalf("want 2 valid fills (3 malformed skipped), got %d: %+v", len(fills), fills)
	}
	btc := fills[0]
	if btc.Coin != "BTC" || btc.Px != 60000.5 || btc.Sz != 0.1 || btc.Side != "B" ||
		btc.Tid != 12345 || btc.Hash != "0xabc" ||
		btc.Buyer != "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" ||
		btc.Seller != "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Errorf("btc fill: %+v", btc)
	}
	eth := fills[1]
	if eth.Coin != "ETH" {
		t.Errorf("coin uppercased: %+v", eth)
	}
	// users=[buyer,seller] always — no swap on side==A.
	if eth.Buyer != "0xcccccccccccccccccccccccccccccccccccccccc" ||
		eth.Seller != "0xdddddddddddddddddddddddddddddddddddddddd" {
		t.Errorf("buyer/seller attribution (no swap): %+v", eth)
	}

	for name, frame := range map[string]string{
		"ack":     `{"channel":"subscriptionResponse","data":{}}`,
		"garbage": `not json`,
	} {
		_, handled, err := parseTradeFillFrame([]byte(frame))
		if name == "ack" && (err != nil || handled) {
			t.Errorf("%s: want ignored", name)
		}
		if name == "garbage" && err == nil {
			t.Errorf("%s: want loud error", name)
		}
	}
}
