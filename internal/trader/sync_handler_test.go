package trader

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

type mockSync struct {
	fn func(ctx context.Context, venue, addr string) (string, error)
}

func (m *mockSync) RequestSync(ctx context.Context, venue, addr string) (string, error) {
	return m.fn(ctx, venue, addr)
}

func syncStatus(t *testing.T, body []byte) string {
	t.Helper()
	var resp struct {
		Success bool `json:"success"`
		Data    struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Success {
		t.Fatalf("success=false: %s", string(body))
	}
	return resp.Data.Status
}

// B1 handler: 202 variants.
func TestHandler_Sync_Queued(t *testing.T) {
	h := NewHandler(&mockService{}).WithSync(&mockSync{
		fn: func(_ context.Context, venue, addr string) (string, error) {
			if venue != "hyperliquid" {
				t.Errorf("default venue must be hyperliquid: %s", venue)
			}
			return SyncPriorityQueued, nil
		},
	})
	w := doReq(t, testRouter(h, ""), "POST",
		"/api/v1/traders/0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/sync", "")
	if w.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d %s", w.Code, w.Body.String())
	}
	if got := syncStatus(t, w.Body.Bytes()); got != "queued" {
		t.Errorf("status: %s", got)
	}
}

func TestHandler_Sync_InFlight(t *testing.T) {
	h := NewHandler(&mockService{}).WithSync(&mockSync{
		fn: func(_ context.Context, _, _ string) (string, error) { return SyncPriorityInFlight, nil },
	})
	w := doReq(t, testRouter(h, ""), "POST",
		"/api/v1/traders/0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/sync", "")
	if w.Code != http.StatusAccepted || syncStatus(t, w.Body.Bytes()) != "in_flight" {
		t.Errorf("got %d %s", w.Code, w.Body.String())
	}
}

func TestHandler_Sync_Recent(t *testing.T) {
	h := NewHandler(&mockService{}).WithSync(&mockSync{
		fn: func(_ context.Context, _, _ string) (string, error) { return SyncPriorityRecent, nil },
	})
	w := doReq(t, testRouter(h, ""), "POST",
		"/api/v1/traders/0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/sync", "")
	if w.Code != http.StatusAccepted || syncStatus(t, w.Body.Bytes()) != "recent" {
		t.Errorf("got %d %s", w.Code, w.Body.String())
	}
}

// B1 handler: 404 unknown wallet (Detail error path).
func TestHandler_Sync_UnknownWallet(t *testing.T) {
	h := NewHandler(&mockService{}).WithSync(&mockSync{
		fn: func(_ context.Context, _, _ string) (string, error) {
			return "", domain.NewError(domain.ErrCodeNotFound, "trader not found")
		},
	})
	w := doReq(t, testRouter(h, ""), "POST",
		"/api/v1/traders/0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/sync", "")
	if w.Code != http.StatusNotFound || errCode(t, w) != "COMMON-903" {
		t.Errorf("got %d %s", w.Code, w.Body.String())
	}
}

// B1 handler: 400 bad address (Detail error path).
func TestHandler_Sync_BadAddress(t *testing.T) {
	h := NewHandler(&mockService{}).WithSync(&mockSync{
		fn: func(_ context.Context, _, _ string) (string, error) {
			return "", domain.NewError(domain.ErrCodeValidation, "invalid wallet address")
		},
	})
	w := doReq(t, testRouter(h, ""), "POST", "/api/v1/traders/not-an-address/sync", "")
	if w.Code != http.StatusBadRequest || errCode(t, w) != "COMMON-902" {
		t.Errorf("got %d %s", w.Code, w.Body.String())
	}
}

// B1 handler: 429 busy when the priority lane is full (COMMON-905).
func TestHandler_Sync_Busy(t *testing.T) {
	h := NewHandler(&mockService{}).WithSync(&mockSync{
		fn: func(_ context.Context, _, _ string) (string, error) {
			return "", domain.NewError(domain.ErrCodeRateLimited, "sync queue full, retry later")
		},
	})
	w := doReq(t, testRouter(h, ""), "POST",
		"/api/v1/traders/0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/sync", "")
	if w.Code != http.StatusTooManyRequests || errCode(t, w) != "COMMON-905" {
		t.Errorf("got %d %s", w.Code, w.Body.String())
	}
}

// B2 handler: activity carries data_status.
func TestHandler_Activity_DataStatus(t *testing.T) {
	for _, st := range []DataStatus{DataReady, DataSyncing, DataStale, DataError} {
		h := NewHandler(&mockService{activityFn: func(_ context.Context, _, _ string, _ ActivityQuery) (*ActivityPage, error) {
			return &ActivityPage{Rows: []ActivityTradeDTO{}, DataStatus: st}, nil
		}})
		w := doReq(t, testRouter(h, ""), "GET",
			"/api/v1/traders/0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/activity", "")
		if w.Code != http.StatusOK {
			t.Fatalf("%s: got %d", st, w.Code)
		}
		data := decodeData(t, w.Body.Bytes())
		if data["data_status"] != string(st) {
			t.Errorf("%s: got %v", st, data["data_status"])
		}
		if data["rows"] == nil {
			t.Errorf("%s: rows must be [] never null", st)
		}
	}
}
