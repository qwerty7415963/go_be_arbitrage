package market

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/api"
)

func setupMarketTestRouter(handler *Handler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(gin.Recovery())

	v1 := router.Group("/api/v1/market")
	{
		v1.GET("/trades", handler.GetTrades)
		v1.GET("/ticker", handler.GetTicker)
		v1.GET("/funding", handler.GetFunding)
		v1.GET("/subscriptions", handler.GetSubscriptions)
	}

	return router
}

func newTestService() *Service {
	return NewService(
		nil,
		nil,
		NewConnectionManager(),
		NewSubscriptionManager(),
	)
}

func parseResponse(t *testing.T, body []byte) api.Response {
	t.Helper()
	var resp api.Response
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	return resp
}

func TestHandler_GetTrades_WhenInvalidVenueID_Returns400(t *testing.T) {
	svc := newTestService()
	handler := NewHandler(svc)
	router := setupMarketTestRouter(handler)

	req, _ := http.NewRequest("GET", "/api/v1/market/trades?venue_id=not-a-uuid&instrument_id="+uuid.New().String(), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}

	resp := parseResponse(t, w.Body.Bytes())
	if resp.Success {
		t.Error("expected success to be false")
	}
	if resp.Error == nil {
		t.Fatal("expected error body")
	}
	if resp.Error.Message != "invalid venue_id" {
		t.Errorf("expected 'invalid venue_id', got %s", resp.Error.Message)
	}
}

func TestHandler_GetTrades_WhenInvalidInstrumentID_Returns400(t *testing.T) {
	svc := newTestService()
	handler := NewHandler(svc)
	router := setupMarketTestRouter(handler)

	req, _ := http.NewRequest("GET", "/api/v1/market/trades?venue_id="+uuid.New().String()+"&instrument_id=bad", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}

	resp := parseResponse(t, w.Body.Bytes())
	if resp.Error == nil {
		t.Fatal("expected error body")
	}
	if resp.Error.Message != "invalid instrument_id" {
		t.Errorf("expected 'invalid instrument_id', got %s", resp.Error.Message)
	}
}

func TestHandler_GetTrades_WhenMissingParams_Returns400(t *testing.T) {
	svc := newTestService()
	handler := NewHandler(svc)
	router := setupMarketTestRouter(handler)

	req, _ := http.NewRequest("GET", "/api/v1/market/trades", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestHandler_GetTicker_WhenInvalidVenueID_Returns400(t *testing.T) {
	svc := newTestService()
	handler := NewHandler(svc)
	router := setupMarketTestRouter(handler)

	req, _ := http.NewRequest("GET", "/api/v1/market/ticker?venue_id=invalid&instrument_id="+uuid.New().String(), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}

	resp := parseResponse(t, w.Body.Bytes())
	if resp.Error == nil {
		t.Fatal("expected error body")
	}
	if resp.Error.Message != "invalid venue_id" {
		t.Errorf("expected 'invalid venue_id', got %s", resp.Error.Message)
	}
}

func TestHandler_GetTicker_WhenInvalidInstrumentID_Returns400(t *testing.T) {
	svc := newTestService()
	handler := NewHandler(svc)
	router := setupMarketTestRouter(handler)

	req, _ := http.NewRequest("GET", "/api/v1/market/ticker?venue_id="+uuid.New().String()+"&instrument_id=not-uuid", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}

	resp := parseResponse(t, w.Body.Bytes())
	if resp.Error == nil {
		t.Fatal("expected error body")
	}
	if resp.Error.Message != "invalid instrument_id" {
		t.Errorf("expected 'invalid instrument_id', got %s", resp.Error.Message)
	}
}

func TestHandler_GetFunding_WhenInvalidVenueID_Returns400(t *testing.T) {
	svc := newTestService()
	handler := NewHandler(svc)
	router := setupMarketTestRouter(handler)

	req, _ := http.NewRequest("GET", "/api/v1/market/funding?venue_id=bad-id&instrument_id="+uuid.New().String(), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}

	resp := parseResponse(t, w.Body.Bytes())
	if resp.Error == nil {
		t.Fatal("expected error body")
	}
	if resp.Error.Message != "invalid venue_id" {
		t.Errorf("expected 'invalid venue_id', got %s", resp.Error.Message)
	}
}

func TestHandler_GetFunding_WhenInvalidInstrumentID_Returns400(t *testing.T) {
	svc := newTestService()
	handler := NewHandler(svc)
	router := setupMarketTestRouter(handler)

	req, _ := http.NewRequest("GET", "/api/v1/market/funding?venue_id="+uuid.New().String()+"&instrument_id=xyz", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}

	resp := parseResponse(t, w.Body.Bytes())
	if resp.Error == nil {
		t.Fatal("expected error body")
	}
	if resp.Error.Message != "invalid instrument_id" {
		t.Errorf("expected 'invalid instrument_id', got %s", resp.Error.Message)
	}
}

func TestHandler_GetSubscriptions_WhenEmpty_Returns200(t *testing.T) {
	svc := newTestService()
	handler := NewHandler(svc)
	router := setupMarketTestRouter(handler)

	req, _ := http.NewRequest("GET", "/api/v1/market/subscriptions", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	resp := parseResponse(t, w.Body.Bytes())
	if !resp.Success {
		t.Error("expected success to be true")
	}
}

func TestHandler_GetSubscriptions_WhenActive_ReturnsSubscriptions(t *testing.T) {
	svc := newTestService()
	handler := NewHandler(svc)
	router := setupMarketTestRouter(handler)

	venueID := uuid.New()
	instrumentID := uuid.New()
	connectionID := uuid.New()

	svc.Subscribe(nil, venueID, instrumentID, "trades", connectionID)

	req, _ := http.NewRequest("GET", "/api/v1/market/subscriptions", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	resp := parseResponse(t, w.Body.Bytes())
	if !resp.Success {
		t.Error("expected success to be true")
	}

	subsRaw, ok := resp.Data.([]interface{})
	if !ok {
		t.Fatalf("expected data to be array, got %T", resp.Data)
	}

	if len(subsRaw) != 1 {
		t.Errorf("expected 1 subscription, got %d", len(subsRaw))
	}
}

func TestHandler_GetTrades_WhenNoParams_Returns400(t *testing.T) {
	svc := newTestService()
	handler := NewHandler(svc)
	router := setupMarketTestRouter(handler)

	req, _ := http.NewRequest("GET", "/api/v1/market/trades?venue_id="+uuid.New().String(), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}
