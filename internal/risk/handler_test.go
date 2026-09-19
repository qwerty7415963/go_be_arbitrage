package risk

import (
	"bytes"
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

const testTenantID = "00000000-0000-0000-0000-000000000001"

func newEngineOnlyService() *Service {
	return NewService(nil, DefaultRiskConfig())
}

func setupTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

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
		t.Skipf("skipping: cannot connect to test DB: %v", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("skipping: cannot ping test DB: %v", err)
	}

	// Ensure tenants table and test tenant exist
	_, _ = pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS tenants (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			name TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'ACTIVE',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)
	_, _ = pool.Exec(ctx, `
		INSERT INTO tenants (id, name, status) VALUES ($1, 'test-tenant', 'ACTIVE')
		ON CONFLICT (id) DO NOTHING
	`, testTenantID)

	// Ensure risk_policies table exists
	_, _ = pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS risk_policies (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			tenant_id UUID NOT NULL REFERENCES tenants(id),
			name TEXT NOT NULL,
			policy_type TEXT NOT NULL,
			config JSONB NOT NULL DEFAULT '{}',
			status TEXT NOT NULL DEFAULT 'ACTIVE',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE(tenant_id, name)
		)
	`)

	t.Cleanup(func() {
		pool.Exec(context.Background(), "DELETE FROM risk_policies WHERE tenant_id = $1", testTenantID)
		pool.Close()
	})

	return pool
}

func newTestServiceWithDB(t *testing.T) *Service {
	t.Helper()
	pool := setupTestDB(t)
	repo := NewRepository(pool)
	return NewService(repo, DefaultRiskConfig())
}

func mockJWTMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("role", "admin")
		c.Set("tenant_id", testTenantID)
		c.Next()
	}
}

func setupHandlerRouter(handler *Handler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(gin.Recovery())

	v1 := router.Group("/api/v1")
	handler.RegisterRoutes(v1, mockJWTMiddleware())

	return router
}

// ─── RISK-H-01: ListPolicies returns 200 ─────────────────────

func TestHandler_ListPolicies_Returns200(t *testing.T) {
	service := newTestServiceWithDB(t)
	handler := NewHandler(service)
	router := setupHandlerRouter(handler)

	req, _ := http.NewRequest("GET", "/api/v1/risk/policies", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["success"] != true {
		t.Error("expected success=true")
	}
}

// ─── RISK-H-02: CreatePolicy returns 201 ─────────────────────

func TestHandler_CreatePolicy_Returns201(t *testing.T) {
	service := newTestServiceWithDB(t)
	handler := NewHandler(service)
	router := setupHandlerRouter(handler)

	body := map[string]interface{}{
		"name":        fmt.Sprintf("test-policy-%d", time.Now().UnixNano()),
		"policy_type": "GLOBAL",
		"config":      DefaultRiskConfig(),
	}
	jsonBody, _ := json.Marshal(body)

	req, _ := http.NewRequest("POST", "/api/v1/risk/policies", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["success"] != true {
		t.Error("expected success=true")
	}
	data := resp["data"].(map[string]interface{})
	if data["id"] == nil {
		t.Error("expected policy id in response")
	}
}

// ─── RISK-H-03: GetPolicy returns 200 ────────────────────────

func TestHandler_GetPolicy_Returns200(t *testing.T) {
	service := newTestServiceWithDB(t)
	handler := NewHandler(service)
	router := setupHandlerRouter(handler)

	createBody := map[string]interface{}{
		"name":        fmt.Sprintf("get-test-%d", time.Now().UnixNano()),
		"policy_type": "GLOBAL",
		"config":      DefaultRiskConfig(),
	}
	createJSON, _ := json.Marshal(createBody)
	createReq, _ := http.NewRequest("POST", "/api/v1/risk/policies", bytes.NewBuffer(createJSON))
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	router.ServeHTTP(createW, createReq)

	if createW.Code != http.StatusCreated {
		t.Fatalf("setup: create policy failed: %d: %s", createW.Code, createW.Body.String())
	}

	var createResp map[string]interface{}
	json.Unmarshal(createW.Body.Bytes(), &createResp)
	policyData := createResp["data"].(map[string]interface{})
	policyID := policyData["id"].(string)

	req, _ := http.NewRequest("GET", "/api/v1/risk/policies/"+policyID, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["success"] != true {
		t.Error("expected success=true")
	}
}

// ─── RISK-H-04: UpdatePolicy returns 200 ─────────────────────

func TestHandler_UpdatePolicy_Returns200(t *testing.T) {
	service := newTestServiceWithDB(t)
	handler := NewHandler(service)
	router := setupHandlerRouter(handler)

	createBody := map[string]interface{}{
		"name":        fmt.Sprintf("update-test-%d", time.Now().UnixNano()),
		"policy_type": "GLOBAL",
		"config":      DefaultRiskConfig(),
	}
	createJSON, _ := json.Marshal(createBody)
	createReq, _ := http.NewRequest("POST", "/api/v1/risk/policies", bytes.NewBuffer(createJSON))
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	router.ServeHTTP(createW, createReq)

	if createW.Code != http.StatusCreated {
		t.Fatalf("setup: create policy failed: %d: %s", createW.Code, createW.Body.String())
	}

	var createResp map[string]interface{}
	json.Unmarshal(createW.Body.Bytes(), &createResp)
	policyData := createResp["data"].(map[string]interface{})
	policyID := policyData["id"].(string)

	updatedName := fmt.Sprintf("updated-%d", time.Now().UnixNano())
	updateBody := map[string]interface{}{
		"name": updatedName,
	}
	updateJSON, _ := json.Marshal(updateBody)
	updateReq, _ := http.NewRequest("PUT", "/api/v1/risk/policies/"+policyID, bytes.NewBuffer(updateJSON))
	updateReq.Header.Set("Content-Type", "application/json")
	updateW := httptest.NewRecorder()
	router.ServeHTTP(updateW, updateReq)

	if updateW.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", updateW.Code, updateW.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(updateW.Body.Bytes(), &resp)
	if resp["success"] != true {
		t.Error("expected success=true")
	}
}

// ─── RISK-H-05: DeletePolicy returns 200 ─────────────────────

func TestHandler_DeletePolicy_Returns200(t *testing.T) {
	service := newTestServiceWithDB(t)
	handler := NewHandler(service)
	router := setupHandlerRouter(handler)

	createBody := map[string]interface{}{
		"name":        fmt.Sprintf("delete-test-%d", time.Now().UnixNano()),
		"policy_type": "GLOBAL",
		"config":      DefaultRiskConfig(),
	}
	createJSON, _ := json.Marshal(createBody)
	createReq, _ := http.NewRequest("POST", "/api/v1/risk/policies", bytes.NewBuffer(createJSON))
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	router.ServeHTTP(createW, createReq)

	if createW.Code != http.StatusCreated {
		t.Fatalf("setup: create policy failed: %d: %s", createW.Code, createW.Body.String())
	}

	var createResp map[string]interface{}
	json.Unmarshal(createW.Body.Bytes(), &createResp)
	policyData := createResp["data"].(map[string]interface{})
	policyID := policyData["id"].(string)

	deleteReq, _ := http.NewRequest("DELETE", "/api/v1/risk/policies/"+policyID, nil)
	deleteW := httptest.NewRecorder()
	router.ServeHTTP(deleteW, deleteReq)

	if deleteW.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", deleteW.Code, deleteW.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(deleteW.Body.Bytes(), &resp)
	if resp["success"] != true {
		t.Error("expected success=true")
	}
}

// ─── RISK-H-06: EnableKillSwitch returns 200 ─────────────────

func TestHandler_EnableKillSwitch_Returns200(t *testing.T) {
	service := newEngineOnlyService()
	handler := NewHandler(service)
	router := setupHandlerRouter(handler)

	body := map[string]string{"reason": "test maintenance"}
	jsonBody, _ := json.Marshal(body)

	req, _ := http.NewRequest("POST", "/api/v1/risk/kill-switch/enable", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["success"] != true {
		t.Error("expected success=true")
	}
}

// ─── RISK-H-07: DisableKillSwitch returns 200 ────────────────

func TestHandler_DisableKillSwitch_Returns200(t *testing.T) {
	service := newEngineOnlyService()
	handler := NewHandler(service)
	router := setupHandlerRouter(handler)

	req, _ := http.NewRequest("POST", "/api/v1/risk/kill-switch/disable", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["success"] != true {
		t.Error("expected success=true")
	}
}

// ─── RISK-H-08: GetKillSwitch returns 200 ────────────────────

func TestHandler_GetKillSwitch_Returns200(t *testing.T) {
	service := newEngineOnlyService()
	handler := NewHandler(service)
	router := setupHandlerRouter(handler)

	req, _ := http.NewRequest("GET", "/api/v1/risk/kill-switch", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["success"] != true {
		t.Error("expected success=true")
	}
	data := resp["data"].(map[string]interface{})
	if data["enabled"] != false {
		t.Error("expected kill switch disabled by default")
	}
}

// ─── RISK-H-09: PreTradeCheck returns 200 ────────────────────

func TestHandler_PreTradeCheck_Returns200(t *testing.T) {
	service := newEngineOnlyService()
	handler := NewHandler(service)
	router := setupHandlerRouter(handler)

	body := map[string]interface{}{
		"tenant_id":        testTenantID,
		"strategy_id":      "00000000-0000-0000-0000-000000000002",
		"venue_account_id": "00000000-0000-0000-0000-000000000003",
		"instrument_id":    "00000000-0000-0000-0000-000000000004",
		"side":             "BUY",
		"quantity":         1.0,
		"price":            50000.0,
		"notional":         50000.0,
	}
	jsonBody, _ := json.Marshal(body)

	req, _ := http.NewRequest("POST", "/api/v1/risk/pre-trade-check", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["success"] != true {
		t.Error("expected success=true")
	}
	data := resp["data"].(map[string]interface{})
	if data["action"] == nil {
		t.Error("expected action in pre-trade check result")
	}
}

// ─── RISK-H-10: CreatePolicy invalid body returns 400 ────────

func TestHandler_CreatePolicy_InvalidBody(t *testing.T) {
	service := newEngineOnlyService()
	handler := NewHandler(service)
	router := setupHandlerRouter(handler)

	req, _ := http.NewRequest("POST", "/api/v1/risk/policies", bytes.NewBuffer([]byte("invalid")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// ─── RISK-H-11: GetPolicy invalid ID returns 400 ─────────────

func TestHandler_GetPolicy_InvalidID(t *testing.T) {
	service := newEngineOnlyService()
	handler := NewHandler(service)
	router := setupHandlerRouter(handler)

	req, _ := http.NewRequest("GET", "/api/v1/risk/policies/not-a-uuid", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// ─── RISK-H-12: GetPolicy not found returns 404 ──────────────

func TestHandler_GetPolicy_NotFound(t *testing.T) {
	service := newTestServiceWithDB(t)
	handler := NewHandler(service)
	router := setupHandlerRouter(handler)

	nonExistentID := uuid.New().String()
	req, _ := http.NewRequest("GET", "/api/v1/risk/policies/"+nonExistentID, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

// ─── RISK-H-13: Non-admin role returns 403 ───────────────────

func TestHandler_NonAdminRole_Returns403(t *testing.T) {
	service := newEngineOnlyService()
	handler := NewHandler(service)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(gin.Recovery())

	v1 := router.Group("/api/v1")
	handler.RegisterRoutes(v1, func(c *gin.Context) {
		c.Set("role", "viewer")
		c.Set("tenant_id", testTenantID)
		c.Next()
	})

	req, _ := http.NewRequest("GET", "/api/v1/risk/policies", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}
