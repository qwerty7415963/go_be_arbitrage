package walletgroup

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// HARD-10: stored group name/color render safely — Go's JSON encoding
// escapes <, > and & so injected markup can never execute in API consumers.
func TestHandler_GroupFields_XSSRenderedSafely(t *testing.T) {
	xssName := `<script>alert(1)</script>`
	xssColor := `" onmouseover="alert(1)`
	gid := uuid.New()

	repo := &mockRepo{
		getGroupByIDFn: func(ctx context.Context, id uuid.UUID) (*Group, error) {
			g := ownerGroup(id)
			g.Name, g.Color = xssName, &xssColor
			return g, nil
		},
	}
	router := setupTestRouter(NewHandler(NewService(repo)))

	w := doJSON(t, router, "GET", "/api/v1/groups/"+gid.String(), userA, nil)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	// No raw angle brackets may survive: encoding/json escapes every <, >
	// and & inside string values, so markup stays inert text.
	if strings.Contains(body, "<") || strings.Contains(body, ">") {
		t.Errorf("raw angle brackets leaked into JSON response: %s", body)
	}
	if !strings.Contains(body, `\u003cscript\u003e`) {
		t.Errorf("expected escaped markup, got: %s", body)
	}
}
