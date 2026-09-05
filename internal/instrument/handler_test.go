package instrument

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func setupTestRouter(handler *Handler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(gin.Recovery())

	v1 := router.Group("/api/v1")
	{
		v1.POST("/instruments", handler.Create)
		v1.GET("/instruments", handler.List)
		v1.GET("/instruments/tradable", handler.ListTradable)
		v1.GET("/instruments/:id", handler.GetByID)
		v1.PUT("/instruments/:id", handler.Update)
		v1.DELETE("/instruments/:id", handler.Delete)
		v1.PUT("/instruments/:id/trading", handler.EnableTrading)
	}

	return router
}

func TestHandler_CreateInstrument_WhenValid_Returns201(t *testing.T) {
	mock := newMockRepository()
	svc := NewService(mock)
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	body := CreateInstrumentRequest{
		CanonicalSymbol: "BTC-USDT",
		BaseAsset:       "BTC",
		QuoteAsset:      "USDT",
		InstrumentType:  InstrumentTypePerp,
		ContractType:    ContractTypeLinear,
		PriceTick:       "0.1",
		QuantityStep:    "0.001",
	}
	jsonBody, _ := json.Marshal(body)

	req, _ := http.NewRequest("POST", "/api/v1/instruments", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp["success"] != true {
		t.Error("expected success to be true")
	}
}

func TestHandler_CreateInstrument_WhenInvalidBody_Returns400(t *testing.T) {
	mock := newMockRepository()
	svc := NewService(mock)
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("POST", "/api/v1/instruments", bytes.NewBuffer([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestHandler_GetByID_WhenFound_Returns200(t *testing.T) {
	mock := newMockRepository()

	inst := &Instrument{
		ID:              uuid.New(),
		CanonicalSymbol: "BTC-USDT",
		BaseAsset:       "BTC",
		QuoteAsset:      "USDT",
		InstrumentType:  InstrumentTypePerp,
		DiscoveryStatus: DiscoveryStatusDiscovered,
		TradingEnabled:  false,
	}
	mock.Create(nil, inst)

	svc := NewService(mock)
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("GET", "/api/v1/instruments/"+inst.ID.String(), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestHandler_GetByID_WhenInvalidID_Returns400(t *testing.T) {
	mock := newMockRepository()
	svc := NewService(mock)
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("GET", "/api/v1/instruments/not-a-uuid", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestHandler_ListTradable_ReturnsTradable(t *testing.T) {
	mock := newMockRepository()

	// Only this one should be in tradable list
	inst1 := &Instrument{
		ID:              uuid.New(),
		CanonicalSymbol: "BTC-USDT",
		TradingEnabled:  true,
		DiscoveryStatus: DiscoveryStatusReviewed,
	}
	inst2 := &Instrument{
		ID:              uuid.New(),
		CanonicalSymbol: "ETH-USDT",
		TradingEnabled:  false,
		DiscoveryStatus: DiscoveryStatusReviewed,
	}

	mock.Create(nil, inst1)
	mock.Create(nil, inst2)

	svc := NewService(mock)
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("GET", "/api/v1/instruments/tradable", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestHandler_EnableTrading_WhenNotReviewed_Returns400(t *testing.T) {
	mock := newMockRepository()

	inst := &Instrument{
		ID:              uuid.New(),
		CanonicalSymbol: "DOGE-USDT",
		DiscoveryStatus: DiscoveryStatusDiscovered, // NOT reviewed
		TradingEnabled:  false,
	}
	mock.Create(nil, inst)

	svc := NewService(mock)
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	body := EnableTradingRequest{Enabled: true}
	jsonBody, _ := json.Marshal(body)

	req, _ := http.NewRequest("PUT", "/api/v1/instruments/"+inst.ID.String()+"/trading", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Should fail because instrument not reviewed
	if w.Code == http.StatusOK {
		t.Error("expected error for non-reviewed instrument, got 200")
	}
}

func TestHandler_EnableTrading_WhenReviewed_Returns200(t *testing.T) {
	mock := newMockRepository()

	inst := &Instrument{
		ID:              uuid.New(),
		CanonicalSymbol: "AVAX-USDT",
		DiscoveryStatus: DiscoveryStatusReviewed, // Reviewed
		TradingEnabled:  false,
	}
	mock.Create(nil, inst)

	svc := NewService(mock)
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	body := EnableTradingRequest{Enabled: true}
	jsonBody, _ := json.Marshal(body)

	req, _ := http.NewRequest("PUT", "/api/v1/instruments/"+inst.ID.String()+"/trading", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestHandler_Delete_WhenValid_Returns204(t *testing.T) {
	mock := newMockRepository()

	inst := &Instrument{
		ID:              uuid.New(),
		CanonicalSymbol: "BTC-USDT",
		TradingEnabled:  false,
		DiscoveryStatus: DiscoveryStatusDiscovered,
	}
	mock.Create(nil, inst)

	svc := NewService(mock)
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("DELETE", "/api/v1/instruments/"+inst.ID.String(), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected status 204, got %d", w.Code)
	}
}
