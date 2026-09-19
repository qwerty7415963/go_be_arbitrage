package strategy

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

type mockRepository struct {
	strategies  map[uuid.UUID]*StrategyInstance
	decisions   map[uuid.UUID][]*StrategyDecision
	strategyTyp map[string]uuid.UUID
}

func newMockRepository() *mockRepository {
	return &mockRepository{
		strategies:  make(map[uuid.UUID]*StrategyInstance),
		decisions:   make(map[uuid.UUID][]*StrategyDecision),
		strategyTyp: make(map[string]uuid.UUID),
	}
}

func (m *mockRepository) Create(ctx context.Context, instance *StrategyInstance) error {
	m.strategies[instance.ID] = instance
	return nil
}

func (m *mockRepository) GetByID(ctx context.Context, id uuid.UUID) (*StrategyInstance, error) {
	s, ok := m.strategies[id]
	if !ok {
		return nil, domain.NewError(domain.ErrCodeNotFound, "strategy not found")
	}
	return s, nil
}

func (m *mockRepository) List(ctx context.Context, tenantID uuid.UUID) ([]*StrategyInstance, error) {
	var instances []*StrategyInstance
	for _, s := range m.strategies {
		if s.TenantID == tenantID {
			instances = append(instances, s)
		}
	}
	return instances, nil
}

func (m *mockRepository) Update(ctx context.Context, instance *StrategyInstance) error {
	m.strategies[instance.ID] = instance
	return nil
}

func (m *mockRepository) Delete(ctx context.Context, id uuid.UUID) error {
	delete(m.strategies, id)
	return nil
}

func (m *mockRepository) CreateDecision(ctx context.Context, decision *StrategyDecision) error {
	m.decisions[decision.StrategyID] = append(m.decisions[decision.StrategyID], decision)
	return nil
}

func (m *mockRepository) ListDecisions(ctx context.Context, strategyID uuid.UUID, limit int) ([]*StrategyDecision, error) {
	return m.decisions[strategyID], nil
}

func (m *mockRepository) GetStrategyTypeByCode(ctx context.Context, code string) (uuid.UUID, error) {
	if id, ok := m.strategyTyp[code]; ok {
		return id, nil
	}
	return uuid.Nil, domain.NewError(domain.ErrCodeNotFound, "strategy type not found")
}

func contextMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("role", "admin")
		c.Set("user_id", uuid.New().String())
		c.Set("tenant_id", uuid.New().String())
		c.Next()
	}
}

func setupTestRouter(handler *Handler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(contextMiddleware())

	v1 := router.Group("/api/v1")
	{
		v1.GET("/strategies", handler.List)
		v1.POST("/strategies", handler.Create)
		v1.GET("/strategies/:id", handler.GetByID)
		v1.PUT("/strategies/:id", handler.Update)
		v1.DELETE("/strategies/:id", handler.Delete)
		v1.POST("/strategies/:id/start", handler.Start)
		v1.POST("/strategies/:id/stop", handler.Stop)
		v1.GET("/strategies/:id/decisions", handler.ListDecisions)
	}

	return router
}

func TestHandler_List_WhenValid_Returns200(t *testing.T) {
	mock := newMockRepository()
	tenantID := uuid.New()

	mock.strategies[uuid.New()] = &StrategyInstance{
		ID:       uuid.New(),
		TenantID: tenantID,
		Name:     "Strategy A",
		Mode:     StrategyModePaper,
		Status:   StrategyStatusDraft,
	}

	svc := &Service{repo: mock, engine: NewEngine()}
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("GET", "/api/v1/strategies", nil)
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

func TestHandler_List_WhenEmpty_ReturnsEmptyList(t *testing.T) {
	mock := newMockRepository()
	svc := &Service{repo: mock, engine: NewEngine()}
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("GET", "/api/v1/strategies", nil)
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

func TestHandler_Create_WhenValid_Returns201(t *testing.T) {
	mock := newMockRepository()
	typeID := uuid.New()
	mock.strategyTyp["PRICE_ARBITRAGE"] = typeID

	svc := &Service{repo: mock, engine: NewEngine()}
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	body := CreateStrategyRequest{
		Name: "Test Strategy",
		Type: StrategyTypePriceArb,
		Mode: StrategyModePaper,
	}
	jsonBody, _ := json.Marshal(body)

	req, _ := http.NewRequest("POST", "/api/v1/strategies", bytes.NewBuffer(jsonBody))
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

func TestHandler_Create_WhenInvalidBody_Returns400(t *testing.T) {
	mock := newMockRepository()
	svc := &Service{repo: mock, engine: NewEngine()}
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("POST", "/api/v1/strategies", bytes.NewBuffer([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestHandler_Create_WhenMissingRequiredFields_Returns400(t *testing.T) {
	mock := newMockRepository()
	svc := &Service{repo: mock, engine: NewEngine()}
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	body := map[string]string{"name": "Test Strategy"}
	jsonBody, _ := json.Marshal(body)

	req, _ := http.NewRequest("POST", "/api/v1/strategies", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestHandler_Create_WhenUnknownStrategyType_Returns500(t *testing.T) {
	mock := newMockRepository()

	svc := &Service{repo: mock, engine: NewEngine()}
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	body := CreateStrategyRequest{
		Name: "Test Strategy",
		Type: StrategyTypePriceArb,
		Mode: StrategyModePaper,
	}
	jsonBody, _ := json.Marshal(body)

	req, _ := http.NewRequest("POST", "/api/v1/strategies", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.Code)
	}
}

func TestHandler_GetByID_WhenFound_Returns200(t *testing.T) {
	mock := newMockRepository()
	tenantID := uuid.New()

	v := &StrategyInstance{
		ID:       uuid.New(),
		TenantID: tenantID,
		Name:     "Test Strategy",
		Mode:     StrategyModePaper,
		Status:   StrategyStatusDraft,
	}
	mock.strategies[v.ID] = v

	svc := &Service{repo: mock, engine: NewEngine()}
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("GET", "/api/v1/strategies/"+v.ID.String(), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestHandler_GetByID_WhenInvalidID_Returns400(t *testing.T) {
	mock := newMockRepository()
	svc := &Service{repo: mock, engine: NewEngine()}
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("GET", "/api/v1/strategies/not-a-uuid", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestHandler_GetByID_WhenNotFound_Returns404(t *testing.T) {
	mock := newMockRepository()
	svc := &Service{repo: mock, engine: NewEngine()}
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	randomID := uuid.New()
	req, _ := http.NewRequest("GET", "/api/v1/strategies/"+randomID.String(), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestHandler_Update_WhenValid_Returns200(t *testing.T) {
	mock := newMockRepository()
	tenantID := uuid.New()

	v := &StrategyInstance{
		ID:       uuid.New(),
		TenantID: tenantID,
		Name:     "Test Strategy",
		Mode:     StrategyModePaper,
		Status:   StrategyStatusDraft,
	}
	mock.strategies[v.ID] = v

	svc := &Service{repo: mock, engine: NewEngine()}
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	updateBody := map[string]string{"name": "Updated Strategy"}
	jsonBody, _ := json.Marshal(updateBody)

	req, _ := http.NewRequest("PUT", "/api/v1/strategies/"+v.ID.String(), bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestHandler_Update_WhenInvalidID_Returns400(t *testing.T) {
	mock := newMockRepository()
	svc := &Service{repo: mock, engine: NewEngine()}
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	updateBody := map[string]string{"name": "Updated Strategy"}
	jsonBody, _ := json.Marshal(updateBody)

	req, _ := http.NewRequest("PUT", "/api/v1/strategies/not-a-uuid", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestHandler_Update_WhenNotFound_Returns500(t *testing.T) {
	mock := newMockRepository()
	svc := &Service{repo: mock, engine: NewEngine()}
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	randomID := uuid.New()
	updateBody := map[string]string{"name": "Updated Strategy"}
	jsonBody, _ := json.Marshal(updateBody)

	req, _ := http.NewRequest("PUT", "/api/v1/strategies/"+randomID.String(), bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.Code)
	}
}

func TestHandler_Delete_WhenValid_Returns200(t *testing.T) {
	mock := newMockRepository()
	tenantID := uuid.New()

	v := &StrategyInstance{
		ID:       uuid.New(),
		TenantID: tenantID,
		Name:     "Test Strategy",
		Mode:     StrategyModePaper,
		Status:   StrategyStatusDraft,
	}
	mock.strategies[v.ID] = v

	svc := &Service{repo: mock, engine: NewEngine()}
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("DELETE", "/api/v1/strategies/"+v.ID.String(), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestHandler_Delete_WhenInvalidID_Returns400(t *testing.T) {
	mock := newMockRepository()
	svc := &Service{repo: mock, engine: NewEngine()}
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("DELETE", "/api/v1/strategies/not-a-uuid", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestHandler_Delete_WhenNotFound_SucceedsIdempotent(t *testing.T) {
	mock := newMockRepository()
	svc := &Service{repo: mock, engine: NewEngine()}
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	randomID := uuid.New()
	req, _ := http.NewRequest("DELETE", "/api/v1/strategies/"+randomID.String(), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestHandler_Start_WhenValid_Returns200(t *testing.T) {
	mock := newMockRepository()
	tenantID := uuid.New()

	v := &StrategyInstance{
		ID:       uuid.New(),
		TenantID: tenantID,
		Name:     "Test Strategy",
		Mode:     StrategyModePaper,
		Status:   StrategyStatusDraft,
	}
	mock.strategies[v.ID] = v

	svc := &Service{repo: mock, engine: NewEngine()}
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("POST", "/api/v1/strategies/"+v.ID.String()+"/start", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	updated, _ := mock.GetByID(context.Background(), v.ID)
	if updated.Status != StrategyStatusRunning {
		t.Errorf("expected status RUNNING, got %s", updated.Status)
	}
}

func TestHandler_Start_WhenAlreadyRunning_Returns500(t *testing.T) {
	mock := newMockRepository()
	tenantID := uuid.New()

	v := &StrategyInstance{
		ID:       uuid.New(),
		TenantID: tenantID,
		Name:     "Test Strategy",
		Mode:     StrategyModePaper,
		Status:   StrategyStatusRunning,
	}
	mock.strategies[v.ID] = v

	svc := &Service{repo: mock, engine: NewEngine()}
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("POST", "/api/v1/strategies/"+v.ID.String()+"/start", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.Code)
	}
}

func TestHandler_Start_WhenNotFound_Returns500(t *testing.T) {
	mock := newMockRepository()
	svc := &Service{repo: mock, engine: NewEngine()}
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	randomID := uuid.New()
	req, _ := http.NewRequest("POST", "/api/v1/strategies/"+randomID.String()+"/start", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.Code)
	}
}

func TestHandler_Start_WhenInvalidID_Returns400(t *testing.T) {
	mock := newMockRepository()
	svc := &Service{repo: mock, engine: NewEngine()}
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("POST", "/api/v1/strategies/not-a-uuid/start", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestHandler_Stop_WhenValid_Returns200(t *testing.T) {
	mock := newMockRepository()
	tenantID := uuid.New()

	v := &StrategyInstance{
		ID:       uuid.New(),
		TenantID: tenantID,
		Name:     "Test Strategy",
		Mode:     StrategyModePaper,
		Status:   StrategyStatusRunning,
	}
	mock.strategies[v.ID] = v

	svc := &Service{repo: mock, engine: NewEngine()}
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("POST", "/api/v1/strategies/"+v.ID.String()+"/stop", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	updated, _ := mock.GetByID(context.Background(), v.ID)
	if updated.Status != StrategyStatusPaused {
		t.Errorf("expected status PAUSED, got %s", updated.Status)
	}
}

func TestHandler_Stop_WhenNotFound_Returns500(t *testing.T) {
	mock := newMockRepository()
	svc := &Service{repo: mock, engine: NewEngine()}
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	randomID := uuid.New()
	req, _ := http.NewRequest("POST", "/api/v1/strategies/"+randomID.String()+"/stop", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.Code)
	}
}

func TestHandler_Stop_WhenInvalidID_Returns400(t *testing.T) {
	mock := newMockRepository()
	svc := &Service{repo: mock, engine: NewEngine()}
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("POST", "/api/v1/strategies/not-a-uuid/stop", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestHandler_ListDecisions_WhenValid_Returns200(t *testing.T) {
	mock := newMockRepository()
	tenantID := uuid.New()

	v := &StrategyInstance{
		ID:       uuid.New(),
		TenantID: tenantID,
		Name:     "Test Strategy",
		Mode:     StrategyModePaper,
		Status:   StrategyStatusDraft,
	}
	mock.strategies[v.ID] = v

	decision := &StrategyDecision{
		ID:         uuid.New(),
		StrategyID: v.ID,
		Action:     "ACCEPT",
		Status:     "PAPER",
		ExecutedAt: time.Now(),
		CreatedAt:  time.Now(),
	}
	mock.decisions[v.ID] = append(mock.decisions[v.ID], decision)

	svc := &Service{repo: mock, engine: NewEngine()}
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("GET", "/api/v1/strategies/"+v.ID.String()+"/decisions", nil)
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

func TestHandler_ListDecisions_WhenInvalidID_Returns400(t *testing.T) {
	mock := newMockRepository()
	svc := &Service{repo: mock, engine: NewEngine()}
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("GET", "/api/v1/strategies/not-a-uuid/decisions", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestHandler_ListDecisions_WhenEmpty_ReturnsEmptyList(t *testing.T) {
	mock := newMockRepository()
	tenantID := uuid.New()

	v := &StrategyInstance{
		ID:       uuid.New(),
		TenantID: tenantID,
		Name:     "Test Strategy",
		Mode:     StrategyModePaper,
		Status:   StrategyStatusDraft,
	}
	mock.strategies[v.ID] = v

	svc := &Service{repo: mock, engine: NewEngine()}
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("GET", "/api/v1/strategies/"+v.ID.String()+"/decisions?limit=10", nil)
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
