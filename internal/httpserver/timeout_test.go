package httpserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestTimeout_SlowHandler_Returns504(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/slow", Timeout(50*time.Millisecond), func(c *gin.Context) {
		time.Sleep(200 * time.Millisecond)
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/slow", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusGatewayTimeout)
	}
	want := `{"success":false,"error":{"code":"GATEWAY_TIMEOUT","message":"request timed out"}}`
	if strings.TrimSpace(w.Body.String()) != want {
		t.Fatalf("body = %q, want %q", w.Body.String(), want)
	}
}

func TestTimeout_FastHandler_Returns200(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/fast", Timeout(50*time.Millisecond), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/fast", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	body := w.Body.String()
	if body == "" {
		t.Fatal("body is empty, want buffered handler response")
	}
	if !strings.Contains(body, `"ok":true`) {
		t.Fatalf("body = %q, want it to contain %q", body, `"ok":true`)
	}
}

func TestTimeout_WSPath_Skipped(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/ws", Timeout(50*time.Millisecond), func(c *gin.Context) {
		time.Sleep(200 * time.Millisecond)
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/ws", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
}
