package unifiedstate

import (
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

	v1 := router.Group("/api/v1/unified")
	{
		v1.GET("/instruments", handler.GetInstruments)
		v1.GET("/instruments/:id", handler.GetInstrument)
		v1.GET("/instruments/:id/depth", handler.GetExecutableDepth)
		v1.GET("/health", handler.GetHealth)
	}
	return router
}

func TestHandler_GetInstruments_Returns200(t *testing.T) {
	svc := NewService(nil)
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("GET", "/api/v1/unified/instruments", nil)
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

func TestHandler_GetInstrument_Found(t *testing.T) {
	svc := NewService(nil)
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	// Seed an instrument state
	instrID := uuid.New()
	svc.GetOrCreateInstrumentState(instrID, "BTC-USDT", "BTC", "USDT")

	req, _ := http.NewRequest("GET", "/api/v1/unified/instruments/"+instrID.String(), nil)
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
	if data["canonical_symbol"] != "BTC-USDT" {
		t.Errorf("expected canonical_symbol BTC-USDT, got %s", data["canonical_symbol"])
	}
}

func TestHandler_GetInstrument_NotFound(t *testing.T) {
	svc := NewService(nil)
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("GET", "/api/v1/unified/instruments/"+uuid.New().String(), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestHandler_GetInstrument_InvalidID(t *testing.T) {
	svc := NewService(nil)
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("GET", "/api/v1/unified/instruments/not-a-uuid", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandler_GetExecutableDepth_InvalidID(t *testing.T) {
	svc := NewService(nil)
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("GET", "/api/v1/unified/instruments/not-a-uuid/depth", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandler_GetExecutableDepth_NotFound(t *testing.T) {
	svc := NewService(nil)
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("GET", "/api/v1/unified/instruments/"+uuid.New().String()+"/depth", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestHandler_GetHealth_Returns200(t *testing.T) {
	svc := NewService(nil)
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("GET", "/api/v1/unified/health", nil)
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
