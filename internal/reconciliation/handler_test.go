package reconciliation

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type mockService struct {
	createRunFn func(ctx context.Context, tenantID, venueAccountID uuid.UUID, trigger TriggerSource) (*ReconciliationRun, error)
	getRunFn    func(ctx context.Context, id uuid.UUID) (*ReconciliationRun, error)
	listRunsFn  func(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]*ReconciliationRun, error)
	listItemsFn func(ctx context.Context, runID uuid.UUID) ([]*ReconciliationItem, error)
}

func (m *mockService) StartReconciliation(ctx context.Context, tenantID, venueAccountID uuid.UUID, trigger TriggerSource) (*ReconciliationRun, error) {
	if m.createRunFn != nil {
		return m.createRunFn(ctx, tenantID, venueAccountID, trigger)
	}
	return &ReconciliationRun{
		ID:             uuid.New(),
		TenantID:       tenantID,
		VenueAccountID: venueAccountID,
		TriggerSource:  trigger,
		Status:         RunStatusRunning,
	}, nil
}

func (m *mockService) GetRun(ctx context.Context, id uuid.UUID) (*ReconciliationRun, error) {
	if m.getRunFn != nil {
		return m.getRunFn(ctx, id)
	}
	return &ReconciliationRun{
		ID:             id,
		TenantID:       uuid.New(),
		VenueAccountID: uuid.New(),
		TriggerSource:  TriggerManual,
		Status:         RunStatusRunning,
	}, nil
}

func (m *mockService) ListRuns(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]*ReconciliationRun, error) {
	if m.listRunsFn != nil {
		return m.listRunsFn(ctx, tenantID, limit, offset)
	}
	return []*ReconciliationRun{}, nil
}

func (m *mockService) ListItems(ctx context.Context, runID uuid.UUID) ([]*ReconciliationItem, error) {
	if m.listItemsFn != nil {
		return m.listItemsFn(ctx, runID)
	}
	return []*ReconciliationItem{}, nil
}

func setupTestRouter(handler *Handler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(gin.Recovery())

	router.Use(func(c *gin.Context) {
		c.Set("role", "admin")
		c.Set("tenant_id", uuid.New().String())
		c.Set("user_id", uuid.New().String())
		c.Next()
	})

	v1 := router.Group("/api/v1")
	{
		recon := v1.Group("/reconciliation")
		recon.Use(func(c *gin.Context) {
			role, exists := c.Get("role")
			if !exists {
				c.JSON(http.StatusForbidden, gin.H{"error": "role not found"})
				c.Abort()
				return
			}
			roleStr, ok := role.(string)
			if !ok || roleStr != "admin" {
				c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
				c.Abort()
				return
			}
			c.Next()
		})
		recon.POST("/runs", handler.CreateRun)
		recon.GET("/runs", handler.ListRuns)
		recon.GET("/runs/:id", handler.GetRun)
		recon.GET("/runs/:id/items", handler.ListItems)
	}

	return router
}

func TestHandler_CreateRun_WhenValid_Returns201(t *testing.T) {
	mock := &mockService{}
	handler := NewHandler(mock)
	router := setupTestRouter(handler)

	body := CreateRunRequest{
		VenueAccountID: uuid.New(),
		TriggerSource:  TriggerManual,
	}
	jsonBody, _ := json.Marshal(body)

	req, _ := http.NewRequest("POST", "/api/v1/reconciliation/runs", bytes.NewBuffer(jsonBody))
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

func TestHandler_ListRuns_WhenValid_Returns200(t *testing.T) {
	tenantID := uuid.New()
	mock := &mockService{
		listRunsFn: func(ctx context.Context, tid uuid.UUID, limit, offset int) ([]*ReconciliationRun, error) {
			return []*ReconciliationRun{
				{
					ID:             uuid.New(),
					TenantID:       tid,
					VenueAccountID: uuid.New(),
					TriggerSource:  TriggerManual,
					Status:         RunStatusMatched,
				},
			}, nil
		},
	}
	handler := NewHandler(mock)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(func(c *gin.Context) {
		c.Set("role", "admin")
		c.Set("tenant_id", tenantID.String())
		c.Set("user_id", uuid.New().String())
		c.Next()
	})

	v1 := router.Group("/api/v1")
	recon := v1.Group("/reconciliation")
	recon.Use(func(c *gin.Context) {
		role, exists := c.Get("role")
		if !exists {
			c.JSON(http.StatusForbidden, gin.H{"error": "role not found"})
			c.Abort()
			return
		}
		roleStr, ok := role.(string)
		if !ok || roleStr != "admin" {
			c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
			c.Abort()
			return
		}
		c.Next()
	})
	recon.GET("/runs", handler.ListRuns)

	req, _ := http.NewRequest("GET", "/api/v1/reconciliation/runs", nil)
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

func TestHandler_GetRun_WhenValid_Returns200(t *testing.T) {
	runID := uuid.New()
	mock := &mockService{
		getRunFn: func(ctx context.Context, id uuid.UUID) (*ReconciliationRun, error) {
			return &ReconciliationRun{
				ID:             id,
				TenantID:       uuid.New(),
				VenueAccountID: uuid.New(),
				TriggerSource:  TriggerManual,
				Status:         RunStatusRunning,
			}, nil
		},
	}
	handler := NewHandler(mock)
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("GET", "/api/v1/reconciliation/runs/"+runID.String(), nil)
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

func TestHandler_ListItems_WhenValid_Returns200(t *testing.T) {
	runID := uuid.New()
	mock := &mockService{
		listItemsFn: func(ctx context.Context, rid uuid.UUID) ([]*ReconciliationItem, error) {
			return []*ReconciliationItem{
				{
					ID:         uuid.New(),
					RunID:      rid,
					EntityType: EntityBalance,
					Result:     ResultMatch,
				},
			}, nil
		},
	}
	handler := NewHandler(mock)
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("GET", "/api/v1/reconciliation/runs/"+runID.String()+"/items", nil)
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
