package telegram

import (
	"strings"
	"testing"
)

func TestMessage_Render_LocaleLabels(t *testing.T) {
	data := map[string]string{
		"group":    "G1",
		"who":      "alice",
		"coin":     "BTC",
		"side":     "LONG",
		"size":     "1.5",
		"price":    "100",
		"notional": "150",
		"time":     "12:00 01/01",
		"link":     "http://x",
	}
	for _, loc := range []Locale{LocaleVI, LocaleEN} {
		out := RenderMessage(MsgLimitPlaced, loc, data)
		if !strings.Contains(out, "LIMIT ORDER PLACED") && loc == LocaleEN {
			// vi uses different label; check vi label
		}
		if loc == LocaleEN && !strings.Contains(out, "LIMIT ORDER PLACED") {
			t.Fatalf("en missing label: %s", out)
		}
		if loc == LocaleVI && !strings.Contains(out, "ĐẶT LỆNH LIMIT") {
			t.Fatalf("vi missing label: %s", out)
		}
		if !strings.Contains(out, "BTC") {
			t.Fatalf("coin not rendered: %s", out)
		}
	}
}

func TestMessage_Render_TakeProfitStopLoss(t *testing.T) {
	base := map[string]string{
		"group": "G1", "who": "alice", "coin": "BTC", "side": "LONG",
		"size": "1", "entry": "100", "exit": "110", "net": "10", "fees": "1",
		"duration": "1h", "link": "http://x",
	}
	tp := RenderMessage(MsgPositionClosed, LocaleEN, base)
	if !strings.Contains(tp, "TAKE PROFIT") {
		t.Fatalf("tp: %s", tp)
	}
	neg := map[string]string{}
	for k, v := range base {
		neg[k] = v
	}
	neg["net"] = "-5"
	sl := RenderMessage(MsgPositionClosed, LocaleEN, neg)
	if !strings.Contains(sl, "STOP LOSS") {
		t.Fatalf("sl: %s", sl)
	}
}

func TestMessage_Render_TP_SL_Placed(t *testing.T) {
	data := map[string]string{"group": "G1", "who": "alice", "coin": "BTC", "side": "LONG", "trigger": "105", "mark": "104"}
	if out := RenderMessage(MsgTPPlaced, LocaleEN, data); !strings.Contains(out, "TAKE PROFIT SET") {
		t.Fatalf("tp placed: %s", out)
	}
	if out := RenderMessage(MsgSLPlaced, LocaleEN, data); !strings.Contains(out, "STOP LOSS SET") {
		t.Fatalf("sl placed: %s", out)
	}
}
