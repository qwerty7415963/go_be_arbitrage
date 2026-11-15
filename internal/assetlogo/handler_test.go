package assetlogo

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func setupRouter(h *Handler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/v1/assets/logos/:assetId", h.ServeLogo)
	r.POST("/api/v1/admin/assets/logos/refresh", h.RefreshLogos)
	r.GET("/api/v1/admin/assets/logos", h.ListLogos)
	return r
}

func TestHandler_ServeLogo_NotFound(t *testing.T) {
	h := NewHandler(nil, NewLocalStorage(t.TempDir()), nil, NewCache())
	// repo nil -> firstLogoRow returns nil
	r := setupRouter(h)
	id := uuid.New()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/assets/logos/"+id.String(), nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("code %d", w.Code)
	}
}

func TestHandler_ServeLogo_BadID(t *testing.T) {
	h := NewHandler(nil, NewLocalStorage(t.TempDir()), nil, NewCache())
	r := setupRouter(h)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/assets/logos/not-uuid", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code %d", w.Code)
	}
}

func TestHandler_RefreshLogos_InvalidID(t *testing.T) {
	h := NewHandler(nil, NewLocalStorage(t.TempDir()), nil, NewCache())
	r := setupRouter(h)
	body, _ := json.Marshal(map[string]interface{}{"asset_ids": []string{"bad"}})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/admin/assets/logos/refresh", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("code %d body %s", w.Code, w.Body.String())
	}
}

func TestHandler_ListLogos_InvalidStatus(t *testing.T) {
	h := NewHandler(nil, NewLocalStorage(t.TempDir()), nil, NewCache())
	r := setupRouter(h)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/admin/assets/logos?status=foo", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code %d", w.Code)
	}
}
