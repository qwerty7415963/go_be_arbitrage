package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestRateLimiter_AllowsWithinLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	limiter := NewRateLimiter(3, time.Minute)
	router.GET("/test", func(c *gin.Context) {
		if !limiter.isAllowed(c.ClientIP()) {
			c.JSON(429, gin.H{"error": "rate limited"})
			return
		}
		c.JSON(200, gin.H{"ok": true})
	})

	for i := 0; i < 3; i++ {
		req, _ := http.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.1:1234"
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("request %d: expected 200, got %d", i+1, w.Code)
		}
	}
}

func TestRateLimiter_BlocksAfterLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	limiter := NewRateLimiter(2, time.Minute)
	router.GET("/test", func(c *gin.Context) {
		if !limiter.isAllowed(c.ClientIP()) {
			c.JSON(429, gin.H{"error": "rate limited"})
			return
		}
		c.JSON(200, gin.H{"ok": true})
	})

	// Use up the limit
	for i := 0; i < 2; i++ {
		req, _ := http.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "10.0.0.1:1234"
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
	}

	// Third request should be blocked
	req, _ := http.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", w.Code)
	}
}

func TestRateLimiter_DifferentIPsIndependent(t *testing.T) {
	limiter := NewRateLimiter(1, time.Minute)

	// IP1 uses its limit
	if !limiter.isAllowed("1.1.1.1:80") {
		t.Error("IP1 should be allowed first time")
	}
	if limiter.isAllowed("1.1.1.1:80") {
		t.Error("IP1 should be blocked after limit")
	}

	// IP2 should still be allowed
	if !limiter.isAllowed("2.2.2.2:80") {
		t.Error("IP2 should be allowed (different IP)")
	}
}

func TestRateLimiter_WindowReset(t *testing.T) {
	limiter := NewRateLimiter(1, 10*time.Millisecond)

	limiter.isAllowed("1.1.1.1:80")
	if limiter.isAllowed("1.1.1.1:80") {
		t.Error("should be blocked immediately after limit")
	}

	// Wait for window to reset
	time.Sleep(15 * time.Millisecond)

	if !limiter.isAllowed("1.1.1.1:80") {
		t.Error("should be allowed after window reset")
	}
}

func TestLoginRateLimit_Middleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	rl := NewRateLimiter(2, time.Minute)
	limiterFunc := func(c *gin.Context) {
		ip := c.ClientIP()
		if !rl.isAllowed(ip) {
			c.JSON(429, gin.H{"error": "rate limited"})
			c.Abort()
			return
		}
		c.Next()
	}
	router.POST("/login", limiterFunc, func(c *gin.Context) {
		c.JSON(200, gin.H{"ok": true})
	})

	// First two requests pass
	for i := 0; i < 2; i++ {
		req, _ := http.NewRequest("POST", "/login", nil)
		req.RemoteAddr = "192.168.1.100:1234"
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("request %d: expected 200, got %d", i+1, w.Code)
		}
	}

	// Third request gets 429
	req, _ := http.NewRequest("POST", "/login", nil)
	req.RemoteAddr = "192.168.1.100:1234"
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", w.Code)
	}
}
