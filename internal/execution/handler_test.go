package execution

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

func setupTestDB(t *testing.T) (*pgxpool.Pool, uuid.UUID, uuid.UUID) {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping handler test requiring database")
	}

	dbHost := os.Getenv("ARBITRAGE_DB_HOST")
	if dbHost == "" {
		dbHost = "localhost"
	}
	dbPort := os.Getenv("ARBITRAGE_DB_PORT")
	if dbPort == "" {
		dbPort = "5433"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	dsn := "postgres://test:test@" + dbHost + ":" + dbPort + "/arbitrage_test?sslmode=disable"

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("failed to connect to database: %v", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("failed to ping database: %v", err)
	}

	pool.Exec(ctx, "DELETE FROM fills")
	pool.Exec(ctx, "DELETE FROM orders")
	pool.Exec(ctx, "DELETE FROM execution_legs")
	pool.Exec(ctx, "DELETE FROM executions")

	var tenantID uuid.UUID
	err = pool.QueryRow(ctx, "SELECT id FROM tenants LIMIT 1").Scan(&tenantID)
	if err != nil {
		tenantID = uuid.New()
		pool.Exec(ctx, "INSERT INTO tenants (id, name, status) VALUES ($1, 'system', 'ACTIVE')", tenantID)
	}

	var strategyTypeID uuid.UUID
	err = pool.QueryRow(ctx, "SELECT id FROM strategy_types WHERE code = 'PRICE_ARBITRAGE'").Scan(&strategyTypeID)
	if err != nil {
		strategyTypeID = uuid.New()
		pool.Exec(ctx, "INSERT INTO strategy_types (id, code, name, status) VALUES ($1, 'PRICE_ARBITRAGE', 'Price Arbitrage', 'ACTIVE')", strategyTypeID)
	}

	strategyInstanceID := uuid.New()
	strategyName := fmt.Sprintf("test-strategy-%s", uuid.New().String()[:8])
	_, err = pool.Exec(ctx, "INSERT INTO strategy_instances (id, tenant_id, strategy_type_id, name, mode, status) VALUES ($1, $2, $3, $4, 'PAPER', 'DRAFT')",
		strategyInstanceID, tenantID, strategyTypeID, strategyName)
	if err != nil {
		t.Fatalf("failed to create strategy instance: %v", err)
	}

	return pool, tenantID, strategyInstanceID
}





func TestHandler_List_WhenNoAuth_ReturnsForbidden(t *testing.T) {
	handler := &Handler{service: nil}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(gin.Recovery())

	v1 := router.Group("/api/v1")
	executions := v1.Group("/executions")
	executions.Use(func(c *gin.Context) {
		role, exists := c.Get("role")
		if !exists {
			c.JSON(http.StatusForbidden, gin.H{"error": "role not found in token"})
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
	executions.GET("", handler.List)

	req, _ := http.NewRequest("GET", "/api/v1/executions", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", w.Code)
	}
}

func TestHandler_List_WhenNonAdmin_ReturnsForbidden(t *testing.T) {
	pool, tenantID, _ := setupTestDB(t)
	defer pool.Close()

	repo := NewRepository(pool)
	svc := NewService(repo)
	handler := NewHandler(svc)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(gin.Recovery())

	v1 := router.Group("/api/v1")
	executions := v1.Group("/executions")
	executions.Use(func(c *gin.Context) {
		c.Set("role", "user")
		c.Set("tenant_id", tenantID.String())
		c.Next()
	})
	executions.Use(func(c *gin.Context) {
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
	executions.GET("", handler.List)

	req, _ := http.NewRequest("GET", "/api/v1/executions", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", w.Code)
	}
}

func TestHandler_List_WhenAdmin_Returns200(t *testing.T) {
	pool, tenantID, _ := setupTestDB(t)
	defer pool.Close()

	repo := NewRepository(pool)
	svc := NewService(repo)
	handler := NewHandler(svc)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(gin.Recovery())

	v1 := router.Group("/api/v1")
	executions := v1.Group("/executions")
	executions.Use(func(c *gin.Context) {
		c.Set("role", "admin")
		c.Set("tenant_id", tenantID.String())
		c.Next()
	})
	executions.Use(func(c *gin.Context) {
		role, _ := c.Get("role")
		if role.(string) != "admin" {
			c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
			c.Abort()
			return
		}
		c.Next()
	})
	executions.GET("", handler.List)

	req, _ := http.NewRequest("GET", "/api/v1/executions", nil)
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
	pool, tenantID, strategyInstanceID := setupTestDB(t)
	defer pool.Close()

	repo := NewRepository(pool)
	svc := NewService(repo)
	handler := NewHandler(svc)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(gin.Recovery())

	v1 := router.Group("/api/v1")
	executions := v1.Group("/executions")
	executions.Use(func(c *gin.Context) {
		c.Set("role", "admin")
		c.Set("tenant_id", tenantID.String())
		c.Next()
	})
	executions.Use(func(c *gin.Context) {
		role, _ := c.Get("role")
		if role.(string) != "admin" {
			c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
			c.Abort()
			return
		}
		c.Next()
	})
	executions.POST("", handler.Create)

	body := CreateExecutionRequest{
		StrategyInstanceID: strategyInstanceID,
		ExecutionMode:      ExecutionModePaper,
		Intent: ExecutionIntent{
			StrategyID: strategyInstanceID,
			Legs: []IntentLeg{
				{
					Side:     SideBuy,
					Quantity: 0.1,
					Price:    50000,
				},
			},
		},
	}

	jsonBody, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", "/api/v1/executions", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["success"] != true {
		t.Error("expected success to be true")
	}
}

func TestHandler_Create_WhenInvalidBody_Returns400(t *testing.T) {
	pool, tenantID, _ := setupTestDB(t)
	defer pool.Close()

	repo := NewRepository(pool)
	svc := NewService(repo)
	handler := NewHandler(svc)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(gin.Recovery())

	v1 := router.Group("/api/v1")
	executions := v1.Group("/executions")
	executions.Use(func(c *gin.Context) {
		c.Set("role", "admin")
		c.Set("tenant_id", tenantID.String())
		c.Next()
	})
	executions.Use(func(c *gin.Context) {
		role, _ := c.Get("role")
		if role.(string) != "admin" {
			c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
			c.Abort()
			return
		}
		c.Next()
	})
	executions.POST("", handler.Create)

	req, _ := http.NewRequest("POST", "/api/v1/executions", bytes.NewBuffer([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestHandler_GetByID_WhenValid_Returns200(t *testing.T) {
	pool, tenantID, strategyInstanceID := setupTestDB(t)
	defer pool.Close()

	repo := NewRepository(pool)
	svc := NewService(repo)
	handler := NewHandler(svc)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(gin.Recovery())

	v1 := router.Group("/api/v1")
	executions := v1.Group("/executions")
	executions.Use(func(c *gin.Context) {
		c.Set("role", "admin")
		c.Set("tenant_id", tenantID.String())
		c.Next()
	})
	executions.Use(func(c *gin.Context) {
		role, _ := c.Get("role")
		if role.(string) != "admin" {
			c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
			c.Abort()
			return
		}
		c.Next()
	})
	executions.POST("", handler.Create)
	executions.GET("/:id", handler.GetByID)

	createBody := CreateExecutionRequest{
		StrategyInstanceID: strategyInstanceID,
		ExecutionMode:      ExecutionModePaper,
		Intent: ExecutionIntent{
			StrategyID: strategyInstanceID,
			Legs: []IntentLeg{
				{
					Side:     SideBuy,
					Quantity: 0.1,
					Price:    50000,
				},
			},
		},
	}

	jsonBody, _ := json.Marshal(createBody)
	createReq, _ := http.NewRequest("POST", "/api/v1/executions", bytes.NewBuffer(jsonBody))
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	router.ServeHTTP(createW, createReq)

	var createResp map[string]interface{}
	json.Unmarshal(createW.Body.Bytes(), &createResp)
	data := createResp["data"].(map[string]interface{})
	execID := data["id"].(string)

	req, _ := http.NewRequest("GET", "/api/v1/executions/"+execID, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandler_GetByID_WhenInvalidID_Returns400(t *testing.T) {
	pool, tenantID, _ := setupTestDB(t)
	defer pool.Close()

	repo := NewRepository(pool)
	svc := NewService(repo)
	handler := NewHandler(svc)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(gin.Recovery())

	v1 := router.Group("/api/v1")
	executions := v1.Group("/executions")
	executions.Use(func(c *gin.Context) {
		c.Set("role", "admin")
		c.Set("tenant_id", tenantID.String())
		c.Next()
	})
	executions.Use(func(c *gin.Context) {
		role, _ := c.Get("role")
		if role.(string) != "admin" {
			c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
			c.Abort()
			return
		}
		c.Next()
	})
	executions.GET("/:id", handler.GetByID)

	req, _ := http.NewRequest("GET", "/api/v1/executions/not-a-uuid", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestHandler_Submit_WhenValid_Returns200(t *testing.T) {
	pool, tenantID, strategyInstanceID := setupTestDB(t)
	defer pool.Close()

	repo := NewRepository(pool)
	svc := NewService(repo)
	handler := NewHandler(svc)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(gin.Recovery())

	v1 := router.Group("/api/v1")
	executions := v1.Group("/executions")
	executions.Use(func(c *gin.Context) {
		c.Set("role", "admin")
		c.Set("tenant_id", tenantID.String())
		c.Next()
	})
	executions.Use(func(c *gin.Context) {
		role, _ := c.Get("role")
		if role.(string) != "admin" {
			c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
			c.Abort()
			return
		}
		c.Next()
	})
	executions.POST("", handler.Create)
	executions.POST("/:id/submit", handler.Submit)

	createBody := CreateExecutionRequest{
		StrategyInstanceID: strategyInstanceID,
		ExecutionMode:      ExecutionModePaper,
		Intent: ExecutionIntent{
			StrategyID: strategyInstanceID,
			Legs: []IntentLeg{
				{
					Side:     SideBuy,
					Quantity: 0.1,
					Price:    50000,
				},
			},
		},
	}

	jsonBody, _ := json.Marshal(createBody)
	createReq, _ := http.NewRequest("POST", "/api/v1/executions", bytes.NewBuffer(jsonBody))
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	router.ServeHTTP(createW, createReq)

	var createResp map[string]interface{}
	json.Unmarshal(createW.Body.Bytes(), &createResp)
	data := createResp["data"].(map[string]interface{})
	execID := data["id"].(string)

	req, _ := http.NewRequest("POST", "/api/v1/executions/"+execID+"/submit", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandler_Cancel_WhenValid_Returns200(t *testing.T) {
	pool, tenantID, strategyInstanceID := setupTestDB(t)
	defer pool.Close()

	repo := NewRepository(pool)
	svc := NewService(repo)
	handler := NewHandler(svc)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(gin.Recovery())

	v1 := router.Group("/api/v1")
	executions := v1.Group("/executions")
	executions.Use(func(c *gin.Context) {
		c.Set("role", "admin")
		c.Set("tenant_id", tenantID.String())
		c.Next()
	})
	executions.Use(func(c *gin.Context) {
		role, _ := c.Get("role")
		if role.(string) != "admin" {
			c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
			c.Abort()
			return
		}
		c.Next()
	})
	executions.POST("", handler.Create)
	executions.POST("/:id/cancel", handler.Cancel)

	createBody := CreateExecutionRequest{
		StrategyInstanceID: strategyInstanceID,
		ExecutionMode:      ExecutionModePaper,
		Intent: ExecutionIntent{
			StrategyID: strategyInstanceID,
			Legs: []IntentLeg{
				{
					Side:     SideBuy,
					Quantity: 0.1,
					Price:    50000,
				},
			},
		},
	}

	jsonBody, _ := json.Marshal(createBody)
	createReq, _ := http.NewRequest("POST", "/api/v1/executions", bytes.NewBuffer(jsonBody))
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	router.ServeHTTP(createW, createReq)

	var createResp map[string]interface{}
	json.Unmarshal(createW.Body.Bytes(), &createResp)
	data := createResp["data"].(map[string]interface{})
	execID := data["id"].(string)

	req, _ := http.NewRequest("POST", "/api/v1/executions/"+execID+"/cancel", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandler_ListLegs_WhenValid_Returns200(t *testing.T) {
	pool, tenantID, strategyInstanceID := setupTestDB(t)
	defer pool.Close()

	repo := NewRepository(pool)
	svc := NewService(repo)
	handler := NewHandler(svc)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(gin.Recovery())

	v1 := router.Group("/api/v1")
	executions := v1.Group("/executions")
	executions.Use(func(c *gin.Context) {
		c.Set("role", "admin")
		c.Set("tenant_id", tenantID.String())
		c.Next()
	})
	executions.Use(func(c *gin.Context) {
		role, _ := c.Get("role")
		if role.(string) != "admin" {
			c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
			c.Abort()
			return
		}
		c.Next()
	})
	executions.POST("", handler.Create)
	executions.GET("/:id/legs", handler.ListLegs)

	createBody := CreateExecutionRequest{
		StrategyInstanceID: strategyInstanceID,
		ExecutionMode:      ExecutionModePaper,
		Intent: ExecutionIntent{
			StrategyID: strategyInstanceID,
			Legs: []IntentLeg{
				{
					Side:     SideBuy,
					Quantity: 0.1,
					Price:    50000,
				},
			},
		},
	}

	jsonBody, _ := json.Marshal(createBody)
	createReq, _ := http.NewRequest("POST", "/api/v1/executions", bytes.NewBuffer(jsonBody))
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	router.ServeHTTP(createW, createReq)

	var createResp map[string]interface{}
	json.Unmarshal(createW.Body.Bytes(), &createResp)
	data := createResp["data"].(map[string]interface{})
	execID := data["id"].(string)

	req, _ := http.NewRequest("GET", "/api/v1/executions/"+execID+"/legs", nil)
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

func TestHandler_ListFills_WhenValid_Returns200(t *testing.T) {
	pool, tenantID, strategyInstanceID := setupTestDB(t)
	defer pool.Close()

	repo := NewRepository(pool)
	svc := NewService(repo)
	handler := NewHandler(svc)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(gin.Recovery())

	v1 := router.Group("/api/v1")
	executions := v1.Group("/executions")
	executions.Use(func(c *gin.Context) {
		c.Set("role", "admin")
		c.Set("tenant_id", tenantID.String())
		c.Next()
	})
	executions.Use(func(c *gin.Context) {
		role, _ := c.Get("role")
		if role.(string) != "admin" {
			c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
			c.Abort()
			return
		}
		c.Next()
	})
	executions.POST("", handler.Create)
	executions.GET("/:id/fills", handler.ListFills)

	createBody := CreateExecutionRequest{
		StrategyInstanceID: strategyInstanceID,
		ExecutionMode:      ExecutionModePaper,
		Intent: ExecutionIntent{
			StrategyID: strategyInstanceID,
			Legs: []IntentLeg{
				{
					Side:     SideBuy,
					Quantity: 0.1,
					Price:    50000,
				},
			},
		},
	}

	jsonBody, _ := json.Marshal(createBody)
	createReq, _ := http.NewRequest("POST", "/api/v1/executions", bytes.NewBuffer(jsonBody))
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	router.ServeHTTP(createW, createReq)

	var createResp map[string]interface{}
	json.Unmarshal(createW.Body.Bytes(), &createResp)
	data := createResp["data"].(map[string]interface{})
	execID := data["id"].(string)

	req, _ := http.NewRequest("GET", "/api/v1/executions/"+execID+"/fills", nil)
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

func TestHandler_Submit_WhenInvalidID_Returns400(t *testing.T) {
	pool, tenantID, _ := setupTestDB(t)
	defer pool.Close()

	repo := NewRepository(pool)
	svc := NewService(repo)
	handler := NewHandler(svc)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(gin.Recovery())

	v1 := router.Group("/api/v1")
	executions := v1.Group("/executions")
	executions.Use(func(c *gin.Context) {
		c.Set("role", "admin")
		c.Set("tenant_id", tenantID.String())
		c.Next()
	})
	executions.Use(func(c *gin.Context) {
		role, _ := c.Get("role")
		if role.(string) != "admin" {
			c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
			c.Abort()
			return
		}
		c.Next()
	})
	executions.POST("/:id/submit", handler.Submit)

	req, _ := http.NewRequest("POST", "/api/v1/executions/not-a-uuid/submit", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestHandler_Cancel_WhenInvalidID_Returns400(t *testing.T) {
	pool, tenantID, _ := setupTestDB(t)
	defer pool.Close()

	repo := NewRepository(pool)
	svc := NewService(repo)
	handler := NewHandler(svc)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(gin.Recovery())

	v1 := router.Group("/api/v1")
	executions := v1.Group("/executions")
	executions.Use(func(c *gin.Context) {
		c.Set("role", "admin")
		c.Set("tenant_id", tenantID.String())
		c.Next()
	})
	executions.Use(func(c *gin.Context) {
		role, _ := c.Get("role")
		if role.(string) != "admin" {
			c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
			c.Abort()
			return
		}
		c.Next()
	})
	executions.POST("/:id/cancel", handler.Cancel)

	req, _ := http.NewRequest("POST", "/api/v1/executions/not-a-uuid/cancel", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestHandler_ListLegs_WhenInvalidID_Returns400(t *testing.T) {
	pool, tenantID, _ := setupTestDB(t)
	defer pool.Close()

	repo := NewRepository(pool)
	svc := NewService(repo)
	handler := NewHandler(svc)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(gin.Recovery())

	v1 := router.Group("/api/v1")
	executions := v1.Group("/executions")
	executions.Use(func(c *gin.Context) {
		c.Set("role", "admin")
		c.Set("tenant_id", tenantID.String())
		c.Next()
	})
	executions.Use(func(c *gin.Context) {
		role, _ := c.Get("role")
		if role.(string) != "admin" {
			c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
			c.Abort()
			return
		}
		c.Next()
	})
	executions.GET("/:id/legs", handler.ListLegs)

	req, _ := http.NewRequest("GET", "/api/v1/executions/not-a-uuid/legs", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestHandler_ListFills_WhenInvalidID_Returns400(t *testing.T) {
	pool, tenantID, _ := setupTestDB(t)
	defer pool.Close()

	repo := NewRepository(pool)
	svc := NewService(repo)
	handler := NewHandler(svc)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(gin.Recovery())

	v1 := router.Group("/api/v1")
	executions := v1.Group("/executions")
	executions.Use(func(c *gin.Context) {
		c.Set("role", "admin")
		c.Set("tenant_id", tenantID.String())
		c.Next()
	})
	executions.Use(func(c *gin.Context) {
		role, _ := c.Get("role")
		if role.(string) != "admin" {
			c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
			c.Abort()
			return
		}
		c.Next()
	})
	executions.GET("/:id/fills", handler.ListFills)

	req, _ := http.NewRequest("GET", "/api/v1/executions/not-a-uuid/fills", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}
