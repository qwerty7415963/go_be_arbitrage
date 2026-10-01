package trader

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

type mockService struct {
	searchFn func(ctx context.Context, userID uuid.UUID, req *SearchRequest) (*SearchResult, error)
	detailFn func(ctx context.Context, venue, addr, period string) (*Detail, error)
}

func (m *mockService) Search(ctx context.Context, userID uuid.UUID, req *SearchRequest) (*SearchResult, error) {
	return m.searchFn(ctx, userID, req)
}

func (m *mockService) Detail(ctx context.Context, venue, addr, period string) (*Detail, error) {
	return m.detailFn(ctx, venue, addr, period)
}

func testRouter(h *Handler, userID string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	v1 := r.Group("/api/v1")
	if userID != "" {
		v1.Use(func(c *gin.Context) {
			c.Set("user_id", userID)
			c.Next()
		})
	}
	h.RegisterRoutes(v1)
	return r
}

func doReq(t *testing.T, r *gin.Engine, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req, _ := http.NewRequest(method, target, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func errCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp["error"].(map[string]any)["code"].(string)
}

// SRCH-H-04: malformed body and service INVALID_FILTER surface as 400.
func TestHandler_Search_InvalidBody(t *testing.T) {
	h := NewHandler(&mockService{})
	w := doReq(t, testRouter(h, ""), "POST", "/api/v1/traders/search", "{bad json")
	if w.Code != http.StatusBadRequest || errCode(t, w) != "INVALID_FILTER" {
		t.Errorf("got %d %s", w.Code, w.Body.String())
	}
}

func TestHandler_Search_ServiceErrors(t *testing.T) {
	h := NewHandler(&mockService{searchFn: func(_ context.Context, _ uuid.UUID, _ *SearchRequest) (*SearchResult, error) {
		return nil, domain.NewError(domain.ErrCodeInvalidFilter, "roi_min > roi_max")
	}})
	w := doReq(t, testRouter(h, ""), "POST", "/api/v1/traders/search", `{"period":"30D"}`)
	if w.Code != http.StatusBadRequest || errCode(t, w) != "INVALID_FILTER" {
		t.Errorf("got %d %s", w.Code, w.Body.String())
	}

	h2 := NewHandler(&mockService{searchFn: func(_ context.Context, _ uuid.UUID, _ *SearchRequest) (*SearchResult, error) {
		return nil, domain.NewError(domain.ErrCodeAuthTokenInvalid, "login needed")
	}})
	w2 := doReq(t, testRouter(h2, ""), "POST", "/api/v1/traders/search", `{"group_id":"11111111-1111-1111-1111-111111111111"}`)
	if w2.Code != http.StatusUnauthorized || errCode(t, w2) != "AUTH-003" {
		t.Errorf("got %d %s", w2.Code, w2.Body.String())
	}
}

// Search success shape: data array + meta cursor/has_more.
func TestHandler_Search_Shape(t *testing.T) {
	pnl := 15000.0
	h := NewHandler(&mockService{searchFn: func(_ context.Context, _ uuid.UUID, _ *SearchRequest) (*SearchResult, error) {
		return &SearchResult{
			Rows:       []*PeriodMetrics{{WalletAddress: "0xabc", PnL: &pnl}},
			NextCursor: "cur123",
			HasMore:    true,
		}, nil
	}})
	w := doReq(t, testRouter(h, ""), "POST", "/api/v1/traders/search", `{"limit":1}`)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}
	var resp struct {
		Data []map[string]any `json:"data"`
		Meta map[string]any   `json:"meta"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Data) != 1 || resp.Data[0]["wallet_address"] != "0xabc" {
		t.Errorf("data: %+v", resp.Data)
	}
	if resp.Meta["cursor"] != "cur123" || resp.Meta["has_more"] != true {
		t.Errorf("meta: %+v", resp.Meta)
	}
}

// TRD-H: detail 404 mapping + bad address 400.
func TestHandler_Detail_Errors(t *testing.T) {
	h := NewHandler(&mockService{detailFn: func(_ context.Context, _, _, _ string) (*Detail, error) {
		return nil, domain.NewError(domain.ErrCodeNotFound, "nope")
	}})
	w := doReq(t, testRouter(h, ""), "GET",
		"/api/v1/traders/0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "")
	if w.Code != http.StatusNotFound || errCode(t, w) != "COMMON-903" {
		t.Errorf("got %d %s", w.Code, w.Body.String())
	}

	h2 := NewHandler(&mockService{detailFn: func(_ context.Context, _, _, _ string) (*Detail, error) {
		return nil, domain.NewError(domain.ErrCodeValidation, "invalid wallet address")
	}})
	w2 := doReq(t, testRouter(h2, ""), "GET", "/api/v1/traders/not-an-address", "")
	if w2.Code != http.StatusBadRequest || errCode(t, w2) != "COMMON-902" {
		t.Errorf("bad address: got %d %s", w2.Code, w2.Body.String())
	}
}
