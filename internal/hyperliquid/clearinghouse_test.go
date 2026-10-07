package hyperliquid

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Fixture-driven: clearinghouseState unmarshal incl. numeric leverage fields.
func TestClearinghouseState_Unmarshal(t *testing.T) {
	raw := `{
		"marginSummary": {"accountValue":"13109.482328","totalNtlPos":"100.02765","totalMarginUsed":"4.967826"},
		"assetPositions": [
			{"type":"oneWay","position":{"coin":"ETH","szi":"0.0335","entryPx":"2986.3",
				"positionValue":"100.02765","unrealizedPnl":"-0.0134","returnOnEquity":"-0.0026789",
				"liquidationPx":"2866.26936529","marginUsed":"4.967826","maxLeverage":50,
				"leverage":{"type":"isolated","value":20,"rawUsd":"-95.059824"}}},
			{"type":"oneWay","position":{"coin":"BTC","szi":"-0.5","entryPx":"43000.0",
				"positionValue":"21625.0","unrealizedPnl":"125.0","returnOnEquity":"5.25",
				"liquidationPx":"38500.0","marginUsed":"4312.50","maxLeverage":"20",
				"leverage":{"type":"cross","value":"2.5"}}}
		],
		"time": 1708622398623
	}`
	var st ClearinghouseState
	if err := json.Unmarshal([]byte(raw), &st); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(st.AssetPositions) != 2 {
		t.Fatalf("positions: %d", len(st.AssetPositions))
	}
	eth := st.AssetPositions[0].Position
	if eth.Coin != "ETH" || string(eth.Szi) != "0.0335" {
		t.Errorf("eth: %+v", eth)
	}
	if string(eth.MaxLeverage) != "50" || string(eth.Leverage.Value) != "20" {
		t.Errorf("numeric leverage not accepted: max=%q val=%q", eth.MaxLeverage, eth.Leverage.Value)
	}
	btc := st.AssetPositions[1].Position
	if string(btc.Szi) != "-0.5" || string(btc.Leverage.Value) != "2.5" {
		t.Errorf("btc: %+v", btc)
	}
	if string(st.MarginSummary.AccountValue) != "13109.482328" {
		t.Errorf("summary: %+v", st.MarginSummary)
	}
}

// FetchClearinghouseState builds the correct request body (httptest server).
func TestFetchClearinghouseState_RequestBody(t *testing.T) {
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/info" {
			t.Errorf("path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"marginSummary":{"accountValue":"10","totalNtlPos":"0","totalMarginUsed":"0"},"assetPositions":[],"time":1}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 5*time.Second, time.Millisecond)
	st, err := c.FetchClearinghouseState(t.Context(), "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if gotBody["type"] != "clearinghouseState" || gotBody["user"] != "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Errorf("body: %v", gotBody)
	}
	if st == nil || len(st.AssetPositions) != 0 {
		t.Errorf("state: %+v", st)
	}
}

// DecimalString accepts both JSON strings and numbers.
func TestDecimalString_StringOrNumber(t *testing.T) {
	var s DecimalString
	if err := json.Unmarshal([]byte(`"12.5"`), &s); err != nil || s != "12.5" {
		t.Errorf("string: %q %v", s, err)
	}
	if err := json.Unmarshal([]byte(`20`), &s); err != nil || s != "20" {
		t.Errorf("int: %q %v", s, err)
	}
	if err := json.Unmarshal([]byte(`2.5`), &s); err != nil || s != "2.5" {
		t.Errorf("float: %q %v", s, err)
	}
	if err := json.Unmarshal([]byte(`null`), &s); err != nil || s != "" {
		t.Errorf("null: %q %v", s, err)
	}
}
