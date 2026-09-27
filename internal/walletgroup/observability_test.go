package walletgroup

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/logger"
)

func setupObservabilityRouter(handler *Handler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		if uid := c.GetHeader("X-Test-User"); uid != "" {
			c.Set("user_id", uid)
		}
		if rid := c.GetHeader("X-Request-ID"); rid != "" {
			c.Set("request_id", rid)
		}
		c.Next()
	})
	handler.RegisterRoutes(router.Group("/api/v1"))
	return router
}

func mutationLogs(t *testing.T, buf *bytes.Buffer) []map[string]interface{} {
	t.Helper()
	var out []map[string]interface{}
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var entry map[string]interface{}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("log line is not JSON: %q", line)
		}
		if entry["msg"] == "group mutation" {
			out = append(out, entry)
		}
	}
	return out
}

// HARD-08: mutations emit structured logs with actor + request id (BE-13);
// validation failures emit none; no secrets ever appear.
func TestHandler_Mutations_StructuredLog(t *testing.T) {
	var buf bytes.Buffer
	log := logger.NewWithWriter("debug", "json", &buf)

	repo := &mockRepo{
		createGroupFn: func(ctx context.Context, g *Group) error {
			g.ID = uuid.New()
			return nil
		},
	}
	router := setupObservabilityRouter(NewHandler(NewService(repo), log))

	w := doJSONWithRequestID(t, router, "POST", "/api/v1/groups", userA, "req-obs-1",
		CreateGroupRequest{Name: "Observed"})
	if w.Code != 201 {
		t.Fatalf("expected 201, got %d", w.Code)
	}

	logs := mutationLogs(t, &buf)
	if len(logs) != 1 {
		t.Fatalf("expected 1 mutation log, got %d", len(logs))
	}
	entry := logs[0]
	if entry["actor"] != userA {
		t.Errorf("actor: expected %s, got %v", userA, entry["actor"])
	}
	if entry["action"] != "group.create" {
		t.Errorf("action: got %v", entry["action"])
	}
	if entry["request_id"] != "req-obs-1" {
		t.Errorf("request_id: got %v", entry["request_id"])
	}
	raw := buf.String()
	for _, secret := range []string{"Bearer", "secret", "token", "password"} {
		if strings.Contains(strings.ToLower(raw), secret) {
			t.Errorf("possible secret in logs: %q", secret)
		}
	}
}

func doJSONWithRequestID(t *testing.T, router *gin.Engine, method, url, userID, requestID string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req, err := http.NewRequest(method, url, bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User", userID)
	req.Header.Set("X-Request-ID", requestID)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// Failed validation must not produce a mutation log line.
func TestHandler_ValidationFailure_NoMutationLog(t *testing.T) {
	var buf bytes.Buffer
	log := logger.NewWithWriter("debug", "json", &buf)
	router := setupObservabilityRouter(NewHandler(NewService(&mockRepo{}), log))

	w := doJSON(t, router, "POST", "/api/v1/groups", userA, CreateGroupRequest{Name: "   "})
	if w.Code != 400 {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if logs := mutationLogs(t, &buf); len(logs) != 0 {
		t.Errorf("expected no mutation logs, got %v", logs)
	}
}
