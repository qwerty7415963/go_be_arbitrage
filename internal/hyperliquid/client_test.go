package hyperliquid

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ING-U-04: a full 2000-fill page triggers window subdivision; the walk
// terminates with no duplicates and no skipped fills.
func TestClient_FetchAll_FullPageSplits(t *testing.T) {
	const narrow = 3

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req fillsByTimeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if req.Type != "userFillsByTime" || req.User == "" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if !req.AggregateByTime {
			http.Error(w, "expected aggregateByTime", http.StatusBadRequest)
			return
		}

		var fills []Fill
		if req.EndTime-req.StartTime > minSliceMs {
			// Full page: force subdivision.
			fills = make([]Fill, maxFillsPerResponse)
			for i := range fills {
				fills[i] = Fill{
					Coin: "BTC", Px: "100", Sz: "0.1", Side: "B",
					Time: req.StartTime + int64(i), StartPosition: "0",
					Dir: "Open Long", ClosedPnl: "0", Fee: "0",
					Tid: req.StartTime*100000 + int64(i),
				}
			}
		} else {
			// Narrow slice: distinct tids derived from the slice start so
			// sibling slices never collide.
			fills = make([]Fill, narrow)
			for i := range fills {
				fills[i] = Fill{
					Coin: "BTC", Px: "100", Sz: "0.1", Side: "B",
					Time: req.StartTime + int64(i), StartPosition: "0",
					Dir: "Open Long", ClosedPnl: "0", Fee: "0",
					Tid: req.StartTime*100000 + int64(i),
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(fills)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 0, 0)
	const eightHours = int64(8 * 3_600_000)

	got, truncated, err := c.FetchAll(context.Background(), "0xabc", 0, eightHours)
	if err != nil {
		t.Fatalf("FetchAll: %v", err)
	}
	if truncated {
		t.Error("narrow slices serve 3 fills — must not flag truncated")
	}
	// 8h → eight 1h slices × 3 fills.
	if len(got) != 8*narrow {
		t.Fatalf("expected %d fills, got %d", 8*narrow, len(got))
	}
	seen := map[int64]bool{}
	for i, f := range got {
		if seen[f.Tid] {
			t.Fatalf("duplicate tid %d at index %d", f.Tid, i)
		}
		seen[f.Tid] = true
		if i > 0 && (got[i-1].Time > f.Time ||
			(got[i-1].Time == f.Time && got[i-1].Tid > f.Tid)) {
			t.Fatalf("not oldest-first at index %d", i)
		}
	}
}

func TestClient_FetchWindow_Decodes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[{"coin":"ETH","px":"50","sz":"2","side":"A","time":1700000000000,`+
			`"startPosition":"0","dir":"Open Short","closedPnl":"0","hash":"0x1",`+
			`"oid":1,"tid":9,"crossed":true,"fee":"0.2","feeToken":"USDC","extra_unknown_field":1}]`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 0, 0)
	got, err := c.FetchWindow(context.Background(), "0xabc", 0, 1)
	if err != nil {
		t.Fatalf("FetchWindow: %v", err)
	}
	if len(got) != 1 || got[0].Coin != "ETH" || got[0].Tid != 9 {
		t.Fatalf("unexpected decode: %+v", got)
	}
}

func TestClient_FetchWindow_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "busy", http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 0, 0)
	if _, err := c.FetchWindow(context.Background(), "0xabc", 0, 1); err == nil {
		t.Fatal("expected error on 429")
	}
}
