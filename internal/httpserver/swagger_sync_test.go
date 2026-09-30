package httpserver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Swagger docs must stay in lockstep with the real HTTP surface: every
// @Router annotation has a path in docs/swagger.json and vice versa, and
// exactly the endpoints that enforce JWT in routes.go carry security.

type swaggerPathItem struct {
	Security []map[string][]string `json:"security"`
}

type swaggerSpec struct {
	SecurityDefinitions map[string]map[string]interface{} `json:"securityDefinitions"`
	Paths               map[string]map[string]swaggerPathItem
}

func loadSwagger(t *testing.T) *swaggerSpec {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "swagger.json"))
	if err != nil {
		t.Fatalf("read swagger.json: %v", err)
	}
	var spec swaggerSpec
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("parse swagger.json: %v", err)
	}
	return &spec
}

// DOCS-S-01: @Router annotations and swagger paths are the same set.
func TestDocs_AnnotationsMatchSwaggerPaths(t *testing.T) {
	spec := loadSwagger(t)

	swagger := map[string]bool{}
	for path, item := range spec.Paths {
		for method := range item {
			swagger[strings.ToUpper(method)+" "+path] = true
		}
	}

	ann := map[string]bool{}
	re := regexp.MustCompile(`@Router\s+(\S+)\s+\[(\w+)\]`)
	for _, root := range []string{filepath.Join("..", "..", "internal"), filepath.Join("..", "..", "cmd")} {
		filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			src, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil
			}
			for _, m := range re.FindAllSubmatch(src, -1) {
				ann[strings.ToUpper(string(m[2]))+" "+string(m[1])] = true
			}
			return nil
		})
	}

	for k := range ann {
		if !swagger[k] {
			t.Errorf("annotation without swagger path: %s", k)
		}
	}
	for k := range swagger {
		if !ann[k] {
			t.Errorf("swagger path without annotation: %s", k)
		}
	}
	if len(ann) != len(swagger) {
		t.Errorf("annotation count %d != swagger path count %d", len(ann), len(swagger))
	}
}

// expectedSecured mirrors routes.go: every group mounted behind
// middleware.JWT (or OptionalJWT+handler check for PATCH wallets).
// Adding a JWT-protected route means adding it here too.
var expectedSecured = []string{
	// auth (JWT-protected subgroup)
	"POST /api/v1/auth/logout",
	"POST /api/v1/auth/change-password",
	"GET /api/v1/auth/me",
	"POST /api/v1/auth/wallet/link",
	"DELETE /api/v1/auth/wallet/{wallet_id}",
	"GET /api/v1/auth/wallet/list",
	// venues (JWT + admin role)
	"GET /api/v1/venues",
	"POST /api/v1/venues",
	"GET /api/v1/venues/{id}",
	"PUT /api/v1/venues/{id}",
	"DELETE /api/v1/venues/{id}",
	// wallet groups (JWT)
	"POST /api/v1/groups",
	"GET /api/v1/groups",
	"GET /api/v1/groups/{id}",
	"PATCH /api/v1/groups/{id}",
	"DELETE /api/v1/groups/{id}",
	"POST /api/v1/groups/{id}/wallets",
	"DELETE /api/v1/groups/{id}/wallets",
	"GET /api/v1/groups/{id}/wallets",
	// wallet scanner: PATCH requires a token inside the handler
	"PATCH /api/v1/wallets/{id}",
	// strategy engine (JWT + admin role)
	"GET /api/v1/strategies",
	"POST /api/v1/strategies",
	"GET /api/v1/strategies/{id}",
	"PUT /api/v1/strategies/{id}",
	"DELETE /api/v1/strategies/{id}",
	"POST /api/v1/strategies/{id}/start",
	"POST /api/v1/strategies/{id}/stop",
	"GET /api/v1/strategies/{id}/decisions",
	// risk engine (JWT + admin role)
	"GET /api/v1/risk/policies",
	"POST /api/v1/risk/policies",
	"GET /api/v1/risk/policies/{id}",
	"PUT /api/v1/risk/policies/{id}",
	"DELETE /api/v1/risk/policies/{id}",
	"POST /api/v1/risk/kill-switch/enable",
	"POST /api/v1/risk/kill-switch/disable",
	"GET /api/v1/risk/kill-switch",
	"POST /api/v1/risk/pre-trade-check",
	// execution engine (JWT + admin role)
	"GET /api/v1/executions",
	"POST /api/v1/executions",
	"GET /api/v1/executions/{id}",
	"POST /api/v1/executions/{id}/submit",
	"POST /api/v1/executions/{id}/cancel",
	"GET /api/v1/executions/{id}/legs",
	"GET /api/v1/executions/{id}/fills",
	// reconciliation engine (JWT + admin role)
	"POST /api/v1/reconciliation/runs",
	"GET /api/v1/reconciliation/runs",
	"GET /api/v1/reconciliation/runs/{id}",
	"GET /api/v1/reconciliation/runs/{id}/items",
}

// DOCS-S-02: exactly the JWT-protected endpoints are documented with
// BearerAuth — no protected route without security, no public route with it.
func TestDocs_SecurityMatchesRoutes(t *testing.T) {
	spec := loadSwagger(t)

	if _, ok := spec.SecurityDefinitions["BearerAuth"]; !ok {
		t.Fatal("securityDefinitions missing BearerAuth")
	}

	secured := map[string]bool{}
	for path, item := range spec.Paths {
		for method, pi := range item {
			if len(pi.Security) > 0 {
				secured[strings.ToUpper(method)+" "+path] = true
			}
		}
	}

	expected := map[string]bool{}
	for _, k := range expectedSecured {
		expected[k] = true
	}

	for _, k := range expectedSecured {
		if !secured[k] {
			t.Errorf("protected endpoint missing @Security in swagger: %s", k)
		}
	}
	for k := range secured {
		if !expected[k] {
			t.Errorf("swagger marks a public endpoint as secured: %s", k)
		}
	}
}
