package trader

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/qwerty7415963/go_be_arbitrage/internal/hyperliquid"
)

func positionsServer(t *testing.T, body string) *hyperliquid.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return hyperliquid.NewClient(srv.URL, 5*time.Second, time.Millisecond)
}

// HLPositionAdapter maps szi sign to LONG/SHORT and derives mark price.
func TestHLPositionAdapter_Mapping(t *testing.T) {
	body := `{"marginSummary":{"accountValue":"1000","totalNtlPos":"500","totalMarginUsed":"100"},
		"assetPositions":[
			{"type":"oneWay","position":{"coin":"BTC","szi":"0.5","entryPx":"60000","positionValue":"31500",
				"unrealizedPnl":"1500","returnOnEquity":"0.05","liquidationPx":"55000","marginUsed":"100",
				"maxLeverage":10,"leverage":{"type":"cross","value":3}}},
			{"type":"oneWay","position":{"coin":"ETH","szi":"-2","entryPx":"3000","positionValue":"6000",
				"unrealizedPnl":"-100","returnOnEquity":"-0.01","liquidationPx":"3500","marginUsed":"200",
				"maxLeverage":10,"leverage":{"type":"cross","value":"5"}}}],
		"time":1}`
	a := HLPositionAdapter{C: positionsServer(t, body)}
	snap, err := a.FetchPositions(context.Background(), "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(snap.Positions) != 2 {
		t.Fatalf("positions: %+v", snap.Positions)
	}
	btc := snap.Positions[0]
	if btc.Side != "LONG" || btc.Size != 0.5 || btc.Coin != "BTC" {
		t.Errorf("btc: %+v", btc)
	}
	if btc.EntryPrice == nil || *btc.EntryPrice != 60000 {
		t.Errorf("entry: %+v", btc.EntryPrice)
	}
	if btc.MarkPrice == nil || *btc.MarkPrice != 63000 {
		t.Errorf("mark derived 31500/0.5=63000: %+v", btc.MarkPrice)
	}
	eth := snap.Positions[1]
	if eth.Side != "SHORT" || eth.Size != 2 {
		t.Errorf("eth: %+v", eth)
	}
	if snap.AccountValue == nil || *snap.AccountValue != 1000 {
		t.Errorf("summary: %+v", snap)
	}
	if snap.AsOf.IsZero() {
		t.Error("AsOf must be set")
	}
}

// Zero/unparseable szi rows are skipped.
func TestHLPositionAdapter_SkipsBadSzi(t *testing.T) {
	body := `{"marginSummary":{"accountValue":"10","totalNtlPos":"0","totalMarginUsed":"0"},
		"assetPositions":[
			{"type":"oneWay","position":{"coin":"BTC","szi":"0","entryPx":"1","positionValue":"0",
				"unrealizedPnl":"0","returnOnEquity":"0","liquidationPx":"1","marginUsed":"0",
				"maxLeverage":10,"leverage":{"type":"cross","value":1}}},
			{"type":"oneWay","position":{"coin":"","szi":"1","entryPx":"1","positionValue":"1",
				"unrealizedPnl":"0","returnOnEquity":"0","liquidationPx":"1","marginUsed":"0",
				"maxLeverage":10,"leverage":{"type":"cross","value":1}}}],
		"time":1}`
	a := HLPositionAdapter{C: positionsServer(t, body)}
	snap, err := a.FetchPositions(context.Background(), "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(snap.Positions) != 0 {
		t.Errorf("bad rows must be skipped: %+v", snap.Positions)
	}
	if snap.Positions == nil {
		t.Error("positions must be [] never nil")
	}
}

func TestPositionSnapshot_NeverNilSlice(t *testing.T) {
	body := `{"marginSummary":{"accountValue":"10","totalNtlPos":"0","totalMarginUsed":"0"},"assetPositions":[],"time":1}`
	a := HLPositionAdapter{C: positionsServer(t, body)}
	snap, err := a.FetchPositions(context.Background(), "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if snap.Positions == nil || len(snap.Positions) != 0 {
		t.Errorf("empty snapshot must be []: %+v", snap.Positions)
	}
	raw, _ := json.Marshal(snap.Positions)
	if string(raw) != "[]" {
		t.Errorf("empty snapshot JSON must equal [], got %s", raw)
	}
}
