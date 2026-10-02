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
