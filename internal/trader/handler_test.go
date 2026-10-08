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
	searchFn      func(ctx context.Context, userID uuid.UUID, req *SearchRequest) (*SearchResult, error)
	detailFn      func(ctx context.Context, venue, addr, period string) (*Detail, error)
	positionsFn   func(ctx context.Context, venue, addr, sort, dir string) (*PositionSnapshotDTO, error)
	activityFn    func(ctx context.Context, venue, addr string, q ActivityQuery) (*ActivityPage, error)
	balancesFn    func(ctx context.Context, venue, addr string) (*BalancesDTO, error)
	fillsFn       func(ctx context.Context, venue, addr string, limit int, cursor string) (*FillsPage, error)
	ordersFn      func(ctx context.Context, venue, addr, status string, limit int) (*OrdersDTO, error)
	transfersFn   func(ctx context.Context, venue, addr string, days, limit int, cursor string) (*TransfersPage, error)
	performanceFn func(ctx context.Context, venue, addr, period string) (*PerformanceDTO, error)
}

func (m *mockService) Search(ctx context.Context, userID uuid.UUID, req *SearchRequest) (*SearchResult, error) {
	return m.searchFn(ctx, userID, req)
}

func (m *mockService) Detail(ctx context.Context, venue, addr, period string) (*Detail, error) {
	return m.detailFn(ctx, venue, addr, period)
}

func (m *mockService) Positions(ctx context.Context, venue, addr, sort, dir string) (*PositionSnapshotDTO, error) {
	if m.positionsFn == nil {
		return nil, domain.NewError(domain.ErrCodeNotFound, "not mocked")
	}
	return m.positionsFn(ctx, venue, addr, sort, dir)
}

func (m *mockService) Activity(ctx context.Context, venue, addr string, q ActivityQuery) (*ActivityPage, error) {
	if m.activityFn == nil {
		return nil, domain.NewError(domain.ErrCodeNotFound, "not mocked")
	}
	return m.activityFn(ctx, venue, addr, q)
}

func (m *mockService) Balances(ctx context.Context, venue, addr string) (*BalancesDTO, error) {
	if m.balancesFn == nil {
		return nil, domain.NewError(domain.ErrCodeNotFound, "not mocked")
	}
	return m.balancesFn(ctx, venue, addr)
}

func (m *mockService) Fills(ctx context.Context, venue, addr string, limit int, cursor string) (*FillsPage, error) {
	if m.fillsFn == nil {
		return nil, domain.NewError(domain.ErrCodeNotFound, "not mocked")
	}
	return m.fillsFn(ctx, venue, addr, limit, cursor)
}

func (m *mockService) Orders(ctx context.Context, venue, addr, status string, limit int) (*OrdersDTO, error) {
	if m.ordersFn == nil {
		return nil, domain.NewError(domain.ErrCodeNotFound, "not mocked")
	}
	return m.ordersFn(ctx, venue, addr, status, limit)
}

func (m *mockService) Transfers(ctx context.Context, venue, addr string, days, limit int, cursor string) (*TransfersPage, error) {
	if m.transfersFn == nil {
		return nil, domain.NewError(domain.ErrCodeNotFound, "not mocked")
	}
	return m.transfersFn(ctx, venue, addr, days, limit, cursor)
}

func (m *mockService) Performance(ctx context.Context, venue, addr, period string) (*PerformanceDTO, error) {
	if m.performanceFn == nil {
		return nil, domain.NewError(domain.ErrCodeNotFound, "not mocked")
	}
	return m.performanceFn(ctx, venue, addr, period)
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

// PG-H-01: numbered page shape — data is that page only; meta carries
// page/total/total_pages alongside limit/has_more.
func TestHandler_Search_PageMeta(t *testing.T) {
	pnl := 25000.0
	h := NewHandler(&mockService{searchFn: func(_ context.Context, _ uuid.UUID, req *SearchRequest) (*SearchResult, error) {
		if req.Page == nil || *req.Page != 2 {
			t.Errorf("page not forwarded: %+v", req.Page)
		}
		return &SearchResult{
			Rows:       []*PeriodMetrics{{WalletAddress: "0xpage2", PnL: &pnl}},
			HasMore:    false,
			Page:       2,
			Total:      3,
			TotalPages: 2,
		}, nil
	}})
	w := doReq(t, testRouter(h, ""), "POST", "/api/v1/traders/search", `{"page":2,"limit":2}`)
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
	if len(resp.Data) != 1 || resp.Data[0]["wallet_address"] != "0xpage2" {
		t.Errorf("data: %+v", resp.Data)
	}
	for k, want := range map[string]any{"page": 2.0, "total": 3.0, "total_pages": 2.0, "limit": 2.0, "has_more": false} {
		if resp.Meta[k] != want {
			t.Errorf("meta[%q]: want %v, got %v (meta=%v)", k, want, resp.Meta[k], resp.Meta)
		}
	}
}

// PG-H-02: service-side page validation surfaces as 400 INVALID_FILTER.
func TestHandler_Search_InvalidPage(t *testing.T) {
	h := NewHandler(&mockService{searchFn: func(_ context.Context, _ uuid.UUID, _ *SearchRequest) (*SearchResult, error) {
		return nil, domain.NewError(domain.ErrCodeInvalidFilter, "page must be >= 1")
	}})
	w := doReq(t, testRouter(h, ""), "POST", "/api/v1/traders/search", `{"page":0,"limit":20}`)
	if w.Code != http.StatusBadRequest || errCode(t, w) != "INVALID_FILTER" {
		t.Errorf("got %d %s", w.Code, w.Body.String())
	}
}

// PG-H-03: non-int page fails binding with the existing 400 shape, before
// the service is ever called.
func TestHandler_Search_NonIntPage(t *testing.T) {
	called := false
	h := NewHandler(&mockService{searchFn: func(_ context.Context, _ uuid.UUID, _ *SearchRequest) (*SearchResult, error) {
		called = true
		return &SearchResult{Rows: []*PeriodMetrics{}}, nil
	}})
	w := doReq(t, testRouter(h, ""), "POST", "/api/v1/traders/search", `{"page":"abc","limit":20}`)
	if w.Code != http.StatusBadRequest || errCode(t, w) != "INVALID_FILTER" {
		t.Errorf("got %d %s", w.Code, w.Body.String())
	}
	if called {
		t.Error("service must not run on bind failure")
	}
}

// PG-H-04: page + cursor in one body is forwarded as-is (service applies the
// page-wins precedence); the handler itself never rejects the combination.
func TestHandler_Search_PageWithCursorForwarded(t *testing.T) {
	h := NewHandler(&mockService{searchFn: func(_ context.Context, _ uuid.UUID, req *SearchRequest) (*SearchResult, error) {
		if req.Page == nil || *req.Page != 1 || req.Cursor != "forged.cursor" {
			t.Errorf("body not forwarded intact: page=%v cursor=%q", req.Page, req.Cursor)
		}
		return &SearchResult{Rows: []*PeriodMetrics{}, Page: 1, Total: 0, TotalPages: 0}, nil
	}})
	w := doReq(t, testRouter(h, ""), "POST", "/api/v1/traders/search", `{"page":1,"cursor":"forged.cursor"}`)
	if w.Code != http.StatusOK {
		t.Errorf("got %d %s", w.Code, w.Body.String())
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
