package opportunity

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/unifiedstate"
)

func setupTestRouter(handler *Handler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(gin.Recovery())

	v1 := router.Group("/api/v1")
	{
		handler.RegisterRoutes(v1)
	}

	return router
}

func newTestService() *Service {
	config := DefaultScannerConfig()
	engine := NewEngine(config)
	return &Service{
		engine: engine,
		config: config,
		stopCh: make(chan struct{}),
	}
}

func seedOpportunity(engine *Engine, oppType OpportunityType) *Opportunity {
	now := time.Now()
	opp := &Opportunity{
		ID:              uuid.New(),
		OpportunityType: oppType,
		Status:          OpportunityStatusDetected,
		InstrumentID:    uuid.New(),
		CanonicalSymbol: "BTC-USDT",
		DetectedAt:      now,
		ExpiresAt:       now.Add(30 * time.Second),
		BuyVenueID:      uuid.New(),
		BuyVenueCode:    "binance",
		SellVenueID:     uuid.New(),
		SellVenueCode:   "okx",
		GrossEdge:       "0.0015",
		GrossEdgeBPS:    15,
		ExpectedNetEdge: "0.0012",
		ExpectedNetEdgeBPS: 12,
		Confidence:      0.85,
		MarketQuality:   MarketQualityGood,
		Legs:            []*OpportunityLeg{},
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	engine.mu.Lock()
	engine.results[opp.ID] = opp
	engine.mu.Unlock()
	return opp
}

func TestListOpportunities_WhenEmpty_Returns200(t *testing.T) {
	svc := newTestService()
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("GET", "/api/v1/opportunities", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp["success"] != true {
		t.Error("expected success to be true")
	}

	data, ok := resp["data"].([]interface{})
	if !ok {
		t.Fatal("expected data to be an array")
	}
	if len(data) != 0 {
		t.Errorf("expected empty list, got %d items", len(data))
	}
}

func TestListOpportunities_WhenPopulated_Returns200(t *testing.T) {
	svc := newTestService()
	seedOpportunity(svc.engine, OpportunityTypePriceArb)
	seedOpportunity(svc.engine, OpportunityTypeFundingArb)
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("GET", "/api/v1/opportunities", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	data, ok := resp["data"].([]interface{})
	if !ok {
		t.Fatal("expected data to be an array")
	}
	if len(data) != 2 {
		t.Errorf("expected 2 opportunities, got %d", len(data))
	}
}

func TestGetOpportunity_WhenFound_Returns200(t *testing.T) {
	svc := newTestService()
	opp := seedOpportunity(svc.engine, OpportunityTypePriceArb)
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("GET", "/api/v1/opportunities/"+opp.ID.String(), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp["success"] != true {
		t.Error("expected success to be true")
	}
}

func TestGetOpportunity_WhenInvalidID_Returns400(t *testing.T) {
	svc := newTestService()
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("GET", "/api/v1/opportunities/not-a-uuid", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestGetOpportunity_WhenNotFound_Returns404(t *testing.T) {
	svc := newTestService()
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	randomID := uuid.New()
	req, _ := http.NewRequest("GET", "/api/v1/opportunities/"+randomID.String(), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestListByType_WhenValidType_Returns200(t *testing.T) {
	svc := newTestService()
	seedOpportunity(svc.engine, OpportunityTypePriceArb)
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("GET", "/api/v1/opportunities/type/PRICE_ARBITRAGE", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	data, ok := resp["data"].([]interface{})
	if !ok {
		t.Fatal("expected data to be an array")
	}
	if len(data) != 1 {
		t.Errorf("expected 1 opportunity, got %d", len(data))
	}
}

func TestListByType_WhenInvalidType_Returns400(t *testing.T) {
	svc := newTestService()
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("GET", "/api/v1/opportunities/type/INVALID_TYPE", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestTriggerScan_WhenNoState_Returns200(t *testing.T) {
	unifiedSvc := unifiedstate.NewService(nil)
	config := DefaultScannerConfig()
	svc := &Service{
		engine:  NewEngine(config),
		unifiedSvc: unifiedSvc,
		config:  config,
		stopCh:  make(chan struct{}),
	}
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("POST", "/api/v1/opportunities/scan", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp["success"] != true {
		t.Error("expected success to be true")
	}
}

func TestRemoveOpportunity_WhenValidID_Returns200(t *testing.T) {
	svc := newTestService()
	opp := seedOpportunity(svc.engine, OpportunityTypePriceArb)
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("DELETE", "/api/v1/opportunities/"+opp.ID.String(), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	remaining := svc.engine.GetAllOpportunities()
	if len(remaining) != 0 {
		t.Errorf("expected 0 opportunities after delete, got %d", len(remaining))
	}
}

func TestRemoveOpportunity_WhenInvalidID_Returns400(t *testing.T) {
	svc := newTestService()
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("DELETE", "/api/v1/opportunities/not-a-uuid", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}
