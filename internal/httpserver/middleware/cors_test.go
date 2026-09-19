package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func setupCORSRouter(origins []string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(CORS(origins))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(200, gin.H{"ok": true})
	})
	router.POST("/test", func(c *gin.Context) {
		c.JSON(200, gin.H{"ok": true})
	})
	return router
}

func TestCORS_Preflight_Returns204(t *testing.T) {
	router := setupCORSRouter([]string{"http://localhost:3000"})

	req, _ := http.NewRequest("OPTIONS", "/test", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", "POST")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != 204 {
		t.Errorf("expected 204, got %d", w.Code)
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "http://localhost:3000" {
		t.Error("expected Allow-Origin header")
	}
}

func TestCORS_AllowedOrigin_SetsHeader(t *testing.T) {
	router := setupCORSRouter([]string{"http://localhost:3000"})

	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "http://localhost:3000" {
		t.Error("expected Allow-Origin header to match")
	}
}

func TestCORS_BlockedOrigin_NoHeader(t *testing.T) {
	router := setupCORSRouter([]string{"http://localhost:3000"})

	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("Origin", "http://evil.com")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error("expected no Allow-Origin header for blocked origin")
	}
}

func TestCORS_Wildcard_AllowsAnyOrigin(t *testing.T) {
	router := setupCORSRouter([]string{"*"})

	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("Origin", "http://anything.com")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Header().Get("Access-Control-Allow-Origin") != "http://anything.com" {
		t.Error("expected wildcard to allow any origin")
	}
}

func TestCORS_CredentialsHeader(t *testing.T) {
	router := setupCORSRouter([]string{"*"})

	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Error("expected credentials header")
	}
}

func TestCORS_MultipleOrigins(t *testing.T) {
	router := setupCORSRouter([]string{"http://localhost:3000", "http://localhost:5173"})

	// First origin
	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Header().Get("Access-Control-Allow-Origin") != "http://localhost:5173" {
		t.Error("expected second origin to be allowed")
	}
}
