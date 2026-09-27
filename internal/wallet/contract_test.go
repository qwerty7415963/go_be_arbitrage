package wallet

import (
	"encoding/json"
	"os"
	"testing"
)

// HARD-07: the shipped OpenAPI contract documents the scanner surface
// (TEST-10) — paths, key filter params and sort enum stay in sync with the
// parser so frontend integration never drifts silently.
func TestSwagger_ScannerContract_Documented(t *testing.T) {
	raw, err := os.ReadFile("../../docs/swagger.json")
	if err != nil {
		t.Fatalf("read swagger.json: %v", err)
	}
	var spec struct {
		Paths map[string]map[string]struct {
			Parameters []struct {
				Name string   `json:"name"`
				In   string   `json:"in"`
				Enum []string `json:"enum"`
			} `json:"parameters"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("parse swagger.json: %v", err)
	}

	get, ok := spec.Paths["/api/v1/wallets"]
	if !ok {
		t.Fatal("swagger missing GET /api/v1/wallets")
	}
	params := map[string]bool{}
	for _, p := range get["get"].Parameters {
		params[p.Name] = true
	}
	for _, want := range []string{
		"search", "dex", "chain", "market", "timeframe", "start", "end",
		"pnl_gt", "pnl_between", "win_rate_gte", "volume_gt",
		"last_active_within", "sort", "order", "page", "limit",
	} {
		if !params[want] {
			t.Errorf("swagger GET /wallets missing documented param %q", want)
		}
	}

	if _, ok := spec.Paths["/api/v1/wallets/{id}"]; !ok {
		t.Error("swagger missing GET /api/v1/wallets/{id}")
	}
}
