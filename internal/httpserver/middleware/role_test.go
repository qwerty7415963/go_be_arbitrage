package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func setupRoleTestRouter(requireRole gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(gin.Recovery())

	v1 := router.Group("/api/v1")
	{
		v1.GET("/admin-only", requireRole, func(c *gin.Context) {
			c.JSON(200, gin.H{"message": "admin ok"})
		})
	}

	return router
}

func withContext(key, value string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(key, value)
		c.Next()
	}
}

// ─── AUTH-M-04: RequireRole allowed ───────────────────────────

func TestRequireRole_AdminAllowed(t *testing.T) {
	req, _ := http.NewRequest("GET", "/api/v1/admin-only", nil)
	w := httptest.NewRecorder()

	// Simulate JWT middleware setting role
	gin.SetMode(gin.TestMode)
	wrappedRouter := gin.New()
	wrappedRouter.Use(gin.Recovery())
	v1 := wrappedRouter.Group("/api/v1")
	v1.GET("/admin-only", withContext("role", "admin"), RequireRole("admin"), func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "admin ok"})
	})

	wrappedRouter.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for admin, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRequireRole_UserAllowed(t *testing.T) {
	req, _ := http.NewRequest("GET", "/api/v1/admin-only", nil)
	w := httptest.NewRecorder()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(gin.Recovery())
	v1 := router.Group("/api/v1")
	v1.GET("/admin-only", withContext("role", "admin"), RequireRole("admin"), func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "admin ok"})
	})

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for admin, got %d", w.Code)
	}
}

// ─── AUTH-M-04: RequireRole denied ────────────────────────────

func TestRequireRole_UserDenied(t *testing.T) {
	req, _ := http.NewRequest("GET", "/api/v1/admin-only", nil)
	w := httptest.NewRecorder()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(gin.Recovery())
	v1 := router.Group("/api/v1")
	v1.GET("/admin-only", withContext("role", "user"), RequireRole("admin"), func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "admin ok"})
	})

	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for user, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRequireRole_MissingRole(t *testing.T) {
	req, _ := http.NewRequest("GET", "/api/v1/admin-only", nil)
	w := httptest.NewRecorder()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(gin.Recovery())
	v1 := router.Group("/api/v1")
	// No context set - role missing
	v1.GET("/admin-only", RequireRole("admin"), func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "admin ok"})
	})

	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 when role missing, got %d", w.Code)
	}
}

func TestRequireRole_NonStringRole(t *testing.T) {
	req, _ := http.NewRequest("GET", "/api/v1/admin-only", nil)
	w := httptest.NewRecorder()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(gin.Recovery())
	v1 := router.Group("/api/v1")
	v1.GET("/admin-only", func(c *gin.Context) {
		c.Set("role", 12345) // not a string
		c.Next()
	}, RequireRole("admin"), func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "admin ok"})
	})

	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for non-string role, got %d", w.Code)
	}
}

// ─── RequireRole: multiple allowed roles ──────────────────────

func TestRequireRole_MultipleRoles_Allowed(t *testing.T) {
	req, _ := http.NewRequest("GET", "/api/v1/admin-only", nil)
	w := httptest.NewRecorder()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(gin.Recovery())
	v1 := router.Group("/api/v1")
	v1.GET("/admin-only", withContext("role", "admin"), RequireRole("admin", "superadmin"), func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "ok"})
	})

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for admin in [admin,superadmin], got %d", w.Code)
	}
}

func TestRequireRole_MultipleRoles_Denied(t *testing.T) {
	req, _ := http.NewRequest("GET", "/api/v1/admin-only", nil)
	w := httptest.NewRecorder()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(gin.Recovery())
	v1 := router.Group("/api/v1")
	v1.GET("/admin-only", withContext("role", "viewer"), RequireRole("admin", "superadmin"), func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "ok"})
	})

	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for viewer, got %d", w.Code)
	}
}
