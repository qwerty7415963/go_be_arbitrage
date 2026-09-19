package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func setupTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping handler test with DB in short mode")
	}

	host := os.Getenv("ARBITRAGE_DB_HOST")
	if host == "" {
		host = "localhost"
	}
	port := os.Getenv("ARBITRAGE_DB_PORT")
	if port == "" {
		port = "5433"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	dsn := fmt.Sprintf("postgres://test:test@%s:%s/arbitrage_test?sslmode=disable", host, port)
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("skipping handler test: failed to connect to test DB: %v", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("skipping handler test: failed to ping test DB: %v", err)
	}

	t.Cleanup(func() {
		pool.Close()
	})

	return pool
}

func setupStorageRouter(handler *Handler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(gin.Recovery())

	v1 := router.Group("/api/v1/storage")
	{
		v1.GET("/opportunities", handler.ListOpportunities)
		v1.GET("/opportunities/:id", handler.GetOpportunity)
		v1.GET("/decisions/strategy/:instance_id", handler.ListStrategyDecisions)
		v1.GET("/decisions/risk/:instance_id", handler.ListRiskDecisions)
		v1.GET("/audit/:tenant_id", handler.ListAuditEvents)
		v1.GET("/replay/market", handler.ReplayMarketEvents)
		v1.POST("/retention/cleanup", handler.CleanupData)
	}

	return router
}

func newTestHandler(pool *pgxpool.Pool) *Handler {
	return NewHandler(
		NewOpportunityRepository(pool),
		NewDecisionRepository(pool),
		NewAuditRepository(pool),
		NewReplayReader(pool),
		NewRetentionService(pool),
	)
}

// ─── ListOpportunities ─────────────────────────────────────────

func TestHandler_ListOpportunities_Returns200(t *testing.T) {
	pool := setupTestDB(t)
	handler := newTestHandler(pool)
	router := setupStorageRouter(handler)

	tenantID := uuid.New()
	req, _ := http.NewRequest("GET", "/api/v1/storage/opportunities?tenant_id="+tenantID.String(), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["success"] != true {
		t.Error("expected success to be true")
	}
}

func TestHandler_ListOpportunities_MissingTenantID_Returns400(t *testing.T) {
	pool := setupTestDB(t)
	handler := newTestHandler(pool)
	router := setupStorageRouter(handler)

	req, _ := http.NewRequest("GET", "/api/v1/storage/opportunities", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestHandler_ListOpportunities_InvalidTenantID_Returns400(t *testing.T) {
	pool := setupTestDB(t)
	handler := newTestHandler(pool)
	router := setupStorageRouter(handler)

	req, _ := http.NewRequest("GET", "/api/v1/storage/opportunities?tenant_id=not-a-uuid", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

// ─── GetOpportunity ────────────────────────────────────────────

func TestHandler_GetOpportunity_Returns200(t *testing.T) {
	pool := setupTestDB(t)
	handler := newTestHandler(pool)
	router := setupStorageRouter(handler)

	tenantID := uuid.New()
	strategyTypeID := uuid.New()
	instrumentID := uuid.New()
	now := time.Now()

	opp := &Opportunity{
		TenantID:           tenantID,
		StrategyTypeID:     strategyTypeID,
		InstrumentID:       instrumentID,
		OpportunityType:    OpportunityTypePriceArb,
		DetectedAt:         now,
		ExpiresAt:          now.Add(1 * time.Hour),
		MarketQuality:      MarketQualityGood,
		CalculationVersion: "v1",
	}
	err := handler.opportunityRepo.Create(context.Background(), opp)
	if err != nil {
		t.Skipf("skipping: failed to seed opportunity: %v", err)
	}

	req, _ := http.NewRequest("GET", "/api/v1/storage/opportunities/"+opp.ID.String(), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["success"] != true {
		t.Error("expected success to be true")
	}
}

func TestHandler_GetOpportunity_InvalidID_Returns400(t *testing.T) {
	pool := setupTestDB(t)
	handler := newTestHandler(pool)
	router := setupStorageRouter(handler)

	req, _ := http.NewRequest("GET", "/api/v1/storage/opportunities/not-a-uuid", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestHandler_GetOpportunity_NotFound_Returns404(t *testing.T) {
	pool := setupTestDB(t)
	handler := newTestHandler(pool)
	router := setupStorageRouter(handler)

	randomID := uuid.New()
	req, _ := http.NewRequest("GET", "/api/v1/storage/opportunities/"+randomID.String(), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

// ─── ListStrategyDecisions (ListDecisions) ─────────────────────

func TestHandler_ListDecisions_Returns200(t *testing.T) {
	pool := setupTestDB(t)
	handler := newTestHandler(pool)
	router := setupStorageRouter(handler)

	instanceID := uuid.New()
	req, _ := http.NewRequest("GET", "/api/v1/storage/decisions/strategy/"+instanceID.String(), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["success"] != true {
		t.Error("expected success to be true")
	}
}

func TestHandler_ListDecisions_InvalidInstanceID_Returns400(t *testing.T) {
	pool := setupTestDB(t)
	handler := newTestHandler(pool)
	router := setupStorageRouter(handler)

	req, _ := http.NewRequest("GET", "/api/v1/storage/decisions/strategy/not-a-uuid", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

// ─── ListAuditEvents ───────────────────────────────────────────

func TestHandler_ListAuditEvents_Returns200(t *testing.T) {
	pool := setupTestDB(t)
	handler := newTestHandler(pool)
	router := setupStorageRouter(handler)

	tenantID := uuid.New()
	req, _ := http.NewRequest("GET", "/api/v1/storage/audit/"+tenantID.String(), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["success"] != true {
		t.Error("expected success to be true")
	}
}

func TestHandler_ListAuditEvents_InvalidTenantID_Returns400(t *testing.T) {
	pool := setupTestDB(t)
	handler := newTestHandler(pool)
	router := setupStorageRouter(handler)

	req, _ := http.NewRequest("GET", "/api/v1/storage/audit/not-a-uuid", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

// ─── CleanupData (TriggerRetention) ────────────────────────────

func TestHandler_TriggerRetention_Returns200(t *testing.T) {
	pool := setupTestDB(t)
	handler := newTestHandler(pool)
	router := setupStorageRouter(handler)

	req, _ := http.NewRequest("POST", "/api/v1/storage/retention/cleanup", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["success"] != true {
		t.Error("expected success to be true")
	}
}

// ─── ReplayMarketEvents ────────────────────────────────────────

func TestHandler_ReplayMarketEvents_InvalidStartTime_Returns400(t *testing.T) {
	pool := setupTestDB(t)
	handler := newTestHandler(pool)
	router := setupStorageRouter(handler)

	req, _ := http.NewRequest("GET", "/api/v1/storage/replay/market?start_time=bad&end_time=bad", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

// ─── ListRiskDecisions ─────────────────────────────────────────

func TestHandler_ListRiskDecisions_Returns200(t *testing.T) {
	pool := setupTestDB(t)
	handler := newTestHandler(pool)
	router := setupStorageRouter(handler)

	instanceID := uuid.New()
	req, _ := http.NewRequest("GET", "/api/v1/storage/decisions/risk/"+instanceID.String(), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["success"] != true {
		t.Error("expected success to be true")
	}
}

func TestHandler_ListRiskDecisions_InvalidInstanceID_Returns400(t *testing.T) {
	pool := setupTestDB(t)
	handler := newTestHandler(pool)
	router := setupStorageRouter(handler)

	req, _ := http.NewRequest("GET", "/api/v1/storage/decisions/risk/not-a-uuid", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

// ─── Handler constructor ───────────────────────────────────────

func TestHandler_NewHandler(t *testing.T) {
	oppRepo := &OpportunityRepository{}
	decRepo := &DecisionRepository{}
	auditRepo := &AuditRepository{}
	replayReader := &ReplayReader{}
	retentionSvc := &RetentionService{}

	handler := NewHandler(oppRepo, decRepo, auditRepo, replayReader, retentionSvc)
	if handler == nil {
		t.Fatal("expected non-nil handler")
	}
	if handler.opportunityRepo != oppRepo {
		t.Error("expected opportunityRepo to be set")
	}
	if handler.decisionRepo != decRepo {
		t.Error("expected decisionRepo to be set")
	}
	if handler.auditRepo != auditRepo {
		t.Error("expected auditRepo to be set")
	}
	if handler.replayReader != replayReader {
		t.Error("expected replayReader to be set")
	}
	if handler.retentionSvc != retentionSvc {
		t.Error("expected retentionSvc to be set")
	}
}
