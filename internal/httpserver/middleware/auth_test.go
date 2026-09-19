package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/qwerty7415963/go_be_arbitrage/internal/auth"
	"github.com/qwerty7415963/go_be_arbitrage/internal/config"
)

func testAuthConfig() *config.AuthConfig {
	return &config.AuthConfig{
		JWTSecret:         "middleware-test-secret",
		JWTExpiration:     15 * time.Minute,
		RefreshExpiration: 7 * 24 * time.Hour,
	}
}

func genToken(svc *auth.Service, userID, tenantID, role string) string {
	pair, _ := svc.GenerateTokenPair(userID, tenantID, role)
	return pair.AccessToken
}

func setupTestMiddlewareRouter(jwtMiddleware gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(gin.Recovery())

	v1 := router.Group("/api/v1")
	{
		v1.GET("/protected", jwtMiddleware, func(c *gin.Context) {
			c.JSON(200, gin.H{
				"user_id":   c.GetString("user_id"),
				"tenant_id": c.GetString("tenant_id"),
				"role":      c.GetString("role"),
			})
		})
	}

	return router
}

// ─── AUTH-M-01: Missing Authorization header ──────────────────

func TestJWT_MissingHeader(t *testing.T) {
	svc := auth.NewService(testAuthConfig(), nil)
	jwtMw := JWT(svc)
	router := setupTestMiddlewareRouter(jwtMw)

	req, _ := http.NewRequest("GET", "/api/v1/protected", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for missing header, got %d: %s", w.Code, w.Body.String())
	}
}

// ─── AUTH-M-01: Invalid header format ─────────────────────────

func TestJWT_InvalidHeaderFormat(t *testing.T) {
	svc := auth.NewService(testAuthConfig(), nil)
	jwtMw := JWT(svc)
	router := setupTestMiddlewareRouter(jwtMw)

	tests := []struct {
		name   string
		header string
	}{
		{"no bearer prefix", "Token abc123"},
		{"bearer only", "Bearer"},
		{"lowercase bearer", "bearer abc123"},
		{"extra spaces", "Bearer  token  abc123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", "/api/v1/protected", nil)
			req.Header.Set("Authorization", tt.header)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != http.StatusUnauthorized {
				t.Errorf("expected 401, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

// ─── AUTH-M-01: Invalid token ─────────────────────────────────

func TestJWT_InvalidToken(t *testing.T) {
	svc := auth.NewService(testAuthConfig(), nil)
	jwtMw := JWT(svc)
	router := setupTestMiddlewareRouter(jwtMw)

	req, _ := http.NewRequest("GET", "/api/v1/protected", nil)
	req.Header.Set("Authorization", "Bearer not-a-real-jwt")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for invalid token, got %d: %s", w.Code, w.Body.String())
	}
}

// ─── AUTH-M-01: Token signed by different secret ──────────────

func TestJWT_WrongSecret(t *testing.T) {
	cfg1 := testAuthConfig()
	cfg2 := &config.AuthConfig{
		JWTSecret:         "completely-different-secret",
		JWTExpiration:     15 * time.Minute,
		RefreshExpiration: 7 * 24 * time.Hour,
	}

	svc1 := auth.NewService(cfg1, nil)
	svc2 := auth.NewService(cfg2, nil)
	token := genToken(svc1, "user-1", "tenant-1", "admin")

	// Use svc2 (different secret) as the middleware's service
	jwtMw := JWT(svc2)
	router := setupTestMiddlewareRouter(jwtMw)

	req, _ := http.NewRequest("GET", "/api/v1/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for wrong secret, got %d", w.Code)
	}
}

// ─── AUTH-M-02: Expired token ─────────────────────────────────

func TestJWT_ExpiredToken(t *testing.T) {
	cfg := &config.AuthConfig{
		JWTSecret:         "middleware-test-secret",
		JWTExpiration:     -1 * time.Second,
		RefreshExpiration: 7 * 24 * time.Hour,
	}

	svc := auth.NewService(cfg, nil)
	token := genToken(svc, "user-1", "tenant-1", "admin")

	// Use a service with valid config for middleware
	validSvc := auth.NewService(testAuthConfig(), nil)
	jwtMw := JWT(validSvc)
	router := setupTestMiddlewareRouter(jwtMw)

	req, _ := http.NewRequest("GET", "/api/v1/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for expired token, got %d", w.Code)
	}
}

// ─── AUTH-M-03: Valid token sets context ──────────────────────

func TestJWT_ValidToken_SetsContext(t *testing.T) {
	svc := auth.NewService(testAuthConfig(), nil)
	token := genToken(svc, "user-abc", "tenant-xyz", "admin")

	jwtMw := JWT(svc)
	router := setupTestMiddlewareRouter(jwtMw)

	req, _ := http.NewRequest("GET", "/api/v1/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)

	if resp["user_id"] != "user-abc" {
		t.Errorf("expected user_id=user-abc, got %v", resp["user_id"])
	}
	if resp["tenant_id"] != "tenant-xyz" {
		t.Errorf("expected tenant_id=tenant-xyz, got %v", resp["tenant_id"])
	}
	if resp["role"] != "admin" {
		t.Errorf("expected role=admin, got %v", resp["role"])
	}
}

func TestJWT_ValidToken_SetsUserContext(t *testing.T) {
	svc := auth.NewService(testAuthConfig(), nil)
	token := genToken(svc, "user-123", "tenant-456", "user")

	jwtMw := JWT(svc)
	router := setupTestMiddlewareRouter(jwtMw)

	req, _ := http.NewRequest("GET", "/api/v1/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)

	if resp["user_id"] != "user-123" {
		t.Errorf("expected user_id=user-123, got %v", resp["user_id"])
	}
	if resp["role"] != "user" {
		t.Errorf("expected role=user, got %v", resp["role"])
	}
}

// ─── Empty token string ───────────────────────────────────────

func TestJWT_EmptyToken(t *testing.T) {
	svc := auth.NewService(testAuthConfig(), nil)
	jwtMw := JWT(svc)
	router := setupTestMiddlewareRouter(jwtMw)

	req, _ := http.NewRequest("GET", "/api/v1/protected", nil)
	req.Header.Set("Authorization", "Bearer ")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for empty token, got %d", w.Code)
	}
}
