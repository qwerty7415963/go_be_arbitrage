package appsettings

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func setupHandlerRouter(h *Handler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(gin.Recovery())
	v1 := router.Group("/api/v1")
	{
		admin := v1.Group("/admin/telegram/settings")
		admin.GET("", h.GetSettings)
		admin.PATCH("", h.UpdateSettings)
	}
	return router
}

func TestHandler_GetSettings(t *testing.T) {
	repo := &mockRepo{val: "50"}
	svc := NewService(repo)
	h := NewHandler(svc)
	router := setupHandlerRouter(h)

	req, _ := http.NewRequest("GET", "/api/v1/admin/telegram/settings", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	data := resp["data"].(map[string]interface{})
	if data["max_wallets_per_user"].(float64) != 50 {
		t.Fatalf("unexpected data: %v", data)
	}
}

func TestHandler_UpdateSettings_Valid(t *testing.T) {
	repo := &mockRepo{val: "50"}
	svc := NewService(repo)
	h := NewHandler(svc)

	body := map[string]int{"max_wallets_per_user": 100}
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("PATCH", "/api/v1/admin/telegram/settings", bytes.NewBuffer(b))
	req.Header.Set("Content-Type", "application/json")
	// provide user_id in context via middleware? handler reads c.Get; set via custom middleware
	router2 := gin.New()
	router2.Use(gin.Recovery())
	router2.Use(func(c *gin.Context) { c.Set("user_id", "00000000-0000-0000-0000-000000000001"); c.Next() })
	v1 := router2.Group("/api/v1")
	{
		admin := v1.Group("/admin/telegram/settings")
		admin.PATCH("", h.UpdateSettings)
	}
	w := httptest.NewRecorder()
	req2, _ := http.NewRequest("PATCH", "/api/v1/admin/telegram/settings", bytes.NewBuffer(b))
	req2.Header.Set("Content-Type", "application/json")
	router2.ServeHTTP(w, req2)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandler_UpdateSettings_Invalid(t *testing.T) {
	repo := &mockRepo{val: "50"}
	svc := NewService(repo)
	h := NewHandler(svc)

	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(func(c *gin.Context) { c.Set("user_id", "00000000-0000-0000-0000-000000000001"); c.Next() })
	v1 := router.Group("/api/v1")
	{
		admin := v1.Group("/admin/telegram/settings")
		admin.PATCH("", h.UpdateSettings)
	}

	body := map[string]int{"max_wallets_per_user": 0}
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("PATCH", "/api/v1/admin/telegram/settings", bytes.NewBuffer(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}
