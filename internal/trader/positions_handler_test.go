package trader

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

func fptrDTO(v float64) *float64 { return &v }

func decodeData(t *testing.T, wBody []byte) map[string]any {
	t.Helper()
	var resp struct {
		Success bool           `json:"success"`
		Data    map[string]any `json:"data"`
		Meta    map[string]any `json:"meta"`
	}
	if err := json.Unmarshal(wBody, &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Success {
		t.Fatalf("success=false: %s", string(wBody))
	}
	return resp.Data
}

// POS-H-01: synced wallet returns summary + rows, positions never null.
func TestHandler_Positions_Success(t *testing.T) {
	now := time.Now().UTC()
	h := NewHandler(&mockService{positionsFn: func(_ context.Context, _, _, _, _ string) (*PositionSnapshotDTO, error) {
		return &PositionSnapshotDTO{
			Summary:    &PositionSummaryDTO{AccountValue: fptrDTO(12345.6), TotalNtlPos: fptrDTO(5000), TotalMarginUsed: fptrDTO(800), AsOf: &now},
			Positions:  []PositionDTO{{Coin: "BTC", Side: "LONG", Size: 0.5}},
			DataStatus: DataReady,
			AsOf:       &now,
		}, nil
	}})
	w := doReq(t, testRouter(h, ""), "GET", "/api/v1/traders/0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/positions?venue=hyperliquid", "")
	if w.Code != http.StatusOK {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}
	data := decodeData(t, w.Body.Bytes())
	if data["data_status"] != "ready" {
		t.Errorf("data_status: %v", data["data_status"])
	}
	rows, ok := data["positions"].([]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("positions: %v", data["positions"])
	}
}

// POS-H-02: unknown wallet -> 404.
func TestHandler_Positions_UnknownWallet(t *testing.T) {
	h := NewHandler(&mockService{positionsFn: func(_ context.Context, _, _, _, _ string) (*PositionSnapshotDTO, error) {
		return nil, domain.NewError(domain.ErrCodeNotFound, "trader not found")
	}})
	w := doReq(t, testRouter(h, ""), "GET", "/api/v1/traders/0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/positions", "")
	if w.Code != http.StatusNotFound || errCode(t, w) != "COMMON-903" {
		t.Errorf("got %d %s", w.Code, w.Body.String())
	}
}

// POS-H-03: invalid address -> 400.
func TestHandler_Positions_InvalidAddress(t *testing.T) {
	h := NewHandler(&mockService{positionsFn: func(_ context.Context, _, _, _, _ string) (*PositionSnapshotDTO, error) {
		return nil, domain.NewError(domain.ErrCodeValidation, "invalid wallet address")
	}})
	w := doReq(t, testRouter(h, ""), "GET", "/api/v1/traders/not-an-address/positions", "")
	if w.Code != http.StatusBadRequest || errCode(t, w) != "COMMON-902" {
		t.Errorf("got %d %s", w.Code, w.Body.String())
	}
}

// POS-H-04: never synced -> syncing, summary null, positions [].
func TestHandler_Positions_NeverSynced(t *testing.T) {
	h := NewHandler(&mockService{positionsFn: func(_ context.Context, _, _, _, _ string) (*PositionSnapshotDTO, error) {
		return &PositionSnapshotDTO{Summary: nil, Positions: []PositionDTO{}, DataStatus: DataSyncing}, nil
	}})
	w := doReq(t, testRouter(h, ""), "GET", "/api/v1/traders/0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/positions", "")
	if w.Code != http.StatusOK {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}
	data := decodeData(t, w.Body.Bytes())
	if data["data_status"] != "syncing" {
		t.Errorf("M5: never synced must be syncing, got %v", data["data_status"])
	}
	if data["summary"] != nil {
		t.Errorf("summary must be null: %v", data["summary"])
	}
	rows, ok := data["positions"].([]any)
	if !ok || len(rows) != 0 {
		t.Errorf("positions must be []: %v", data["positions"])
	}
}

// ACT-H-01: default limit 20, net_pnl computed, has_more + next_cursor.
func TestHandler_Activity_Success(t *testing.T) {
	h := NewHandler(&mockService{activityFn: func(_ context.Context, _, _ string, q ActivityQuery) (*ActivityPage, error) {
		if q.Limit != 20 {
			t.Errorf("default limit must be 20, got %d", q.Limit)
		}
		now := time.Now().UTC()
		return &ActivityPage{
			Rows: []ActivityTradeDTO{
				{Market: "BTC", Side: "LONG", OpenedAt: now.Add(-time.Hour), ClosedAt: now, Volume: 30000, PnL: 1500, Fees: 30, NetPnl: 1470, Fills: 3},
			},
			NextCursor: "opaque", HasMore: true,
		}, nil
	}})
	w := doReq(t, testRouter(h, ""), "GET", "/api/v1/traders/0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/activity", "")
	if w.Code != http.StatusOK {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}
	data := decodeData(t, w.Body.Bytes())
	rows := data["rows"].([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["net_pnl"] != 1470.0 {
		t.Errorf("net_pnl: %v", rows)
	}
	if data["has_more"] != true || data["next_cursor"] != "opaque" {
		t.Errorf("pagination: %v", data)
	}
}

// ACT-H-02: limit=0 / 101 -> 400. Handler passes through; service rejects.
func TestHandler_Activity_BadLimit(t *testing.T) {
	for _, target := range []string{
		"/api/v1/traders/0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/activity?limit=0",
		"/api/v1/traders/0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/activity?limit=101",
		"/api/v1/traders/0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/activity?limit=abc",
	} {
		h := NewHandler(&mockService{activityFn: func(_ context.Context, _, _ string, _ ActivityQuery) (*ActivityPage, error) {
			return nil, domain.NewError(domain.ErrCodeInvalidFilter, "limit must be 1..100")
		}})
		w := doReq(t, testRouter(h, ""), "GET", target, "")
		if w.Code != http.StatusBadRequest || errCode(t, w) != "INVALID_FILTER" {
			t.Errorf("%s: got %d %s", target, w.Code, w.Body.String())
		}
	}
}

// ACT-H-03: tampered cursor -> 400.
func TestHandler_Activity_BadCursor(t *testing.T) {
	h := NewHandler(&mockService{activityFn: func(_ context.Context, _, _ string, _ ActivityQuery) (*ActivityPage, error) {
		return nil, domain.NewError(domain.ErrCodeInvalidFilter, "invalid cursor signature")
	}})
	w := doReq(t, testRouter(h, ""), "GET", "/api/v1/traders/0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/activity?cursor=forged.cursor", "")
	if w.Code != http.StatusBadRequest || errCode(t, w) != "INVALID_FILTER" {
		t.Errorf("got %d %s", w.Code, w.Body.String())
	}
}

// ACT-H-04: unknown wallet -> 404.
func TestHandler_Activity_UnknownWallet(t *testing.T) {
	h := NewHandler(&mockService{activityFn: func(_ context.Context, _, _ string, _ ActivityQuery) (*ActivityPage, error) {
		return nil, domain.NewError(domain.ErrCodeNotFound, "trader not found")
	}})
	w := doReq(t, testRouter(h, ""), "GET", "/api/v1/traders/0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/activity", "")
	if w.Code != http.StatusNotFound {
		t.Errorf("got %d %s", w.Code, w.Body.String())
	}
}
