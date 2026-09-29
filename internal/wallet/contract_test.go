package wallet

import (
	"encoding/json"
	"os"
	"strings"
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
				Name    string   `json:"name"`
				In      string   `json:"in"`
				Enum    []string `json:"enum"`
				Default any      `json:"default"`
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
	byName := map[string]struct {
		Enum    []string `json:"enum"`
		Default any      `json:"default"`
	}{}
	params := map[string]bool{}
	for _, p := range get["get"].Parameters {
		params[p.Name] = true
		byName[p.Name] = struct {
			Enum    []string `json:"enum"`
			Default any      `json:"default"`
		}{p.Enum, p.Default}
	}
	for _, want := range []string{
		"search", "dex", "chain", "market", "timeframe", "start", "end",
		"pnl_gt", "pnl_between", "win_rate_gte", "volume_gt",
		"last_active_within", "sort", "order", "page", "limit",
		"watchlisted", // WL-*: star filter documented for the frontend
	} {
		if !params[want] {
			t.Errorf("swagger GET /wallets missing documented param %q", want)
		}
	}

	// SCAN-H-21: closed sets carry enums + defaults in the schema.
	assertEnum := func(name string, wantEnum []string, wantDefault string) {
		t.Helper()
		got, ok := byName[name]
		if !ok {
			t.Errorf("swagger missing param %q", name)
			return
		}
		if len(got.Enum) != len(wantEnum) {
			t.Errorf("%s enum: expected %v, got %v", name, wantEnum, got.Enum)
			return
		}
		for i := range wantEnum {
			if got.Enum[i] != wantEnum[i] {
				t.Errorf("%s enum: expected %v, got %v", name, wantEnum, got.Enum)
				break
			}
		}
		if def, _ := got.Default.(string); def != wantDefault {
			t.Errorf("%s default: expected %q, got %v", name, wantDefault, got.Default)
		}
	}
	assertEnum("timeframe", []string{"24H", "7D", "30D", "90D", "ALL"}, "30D")
	assertEnum("sort", []string{"pnl", "roi", "win_rate", "volume", "trade_count", "avg_position", "avg_leverage", "last_active"}, "pnl")
	assertEnum("order", []string{"asc", "desc"}, "desc")

	// All 8 metrics × 5 ops documented (symmetric ops).
	for _, m := range []string{"pnl", "roi", "win_rate", "volume", "trade_count", "avg_position", "avg_leverage", "long_short_ratio"} {
		for _, op := range []string{"gt", "gte", "lt", "lte", "between"} {
			if !params[m+"_"+op] {
				t.Errorf("swagger GET /wallets missing param %q", m+"_"+op)
			}
		}
	}

	if _, ok := spec.Paths["/api/v1/wallets/{id}"]; !ok {
		t.Error("swagger missing GET /api/v1/wallets/{id}")
	}
	if _, ok := spec.Paths["/api/v1/wallets/{id}"]["patch"]; !ok {
		t.Error("swagger missing PATCH /api/v1/wallets/{id}")
	}

	// CFG-H-01 contract: filter-config endpoint documented.
	if _, ok := spec.Paths["/api/v1/wallets/filter-config"]["get"]; !ok {
		t.Error("swagger missing GET /api/v1/wallets/filter-config")
	}

	// Schema contract: PATCH body carries watchlisted; detail carries
	// positions (WL-*/POS-*), so the frontend can rely on the shapes.
	var full struct {
		Definitions map[string]struct {
			Properties map[string]json.RawMessage `json:"properties"`
		} `json:"definitions"`
	}
	if err := json.Unmarshal(raw, &full); err != nil {
		t.Fatalf("parse definitions: %v", err)
	}
	patchBody, ok := full.Definitions["internal_wallet.UpdateTagRequest"]
	if !ok {
		t.Error("swagger missing UpdateTagRequest definition")
	} else if _, ok := patchBody.Properties["watchlisted"]; !ok {
		t.Error("UpdateTagRequest missing watchlisted property")
	}
	detail, ok := full.Definitions["internal_wallet.WalletDetail"]
	if !ok {
		t.Error("swagger missing WalletDetail definition")
	} else {
		if _, ok := detail.Properties["positions"]; !ok {
			t.Error("WalletDetail missing positions property")
		}
		if _, ok := detail.Properties["memberships"]; !ok {
			t.Error("WalletDetail missing memberships property")
		}
	}
	if _, ok := full.Definitions["internal_wallet.ScannerConfig"]; !ok {
		t.Error("swagger missing ScannerConfig definition")
	}
	if row, ok := full.Definitions["internal_wallet.Wallet"]; ok {
		if _, ok := row.Properties["watchlisted"]; !ok {
			t.Error("Wallet missing watchlisted property")
		}
	} else {
		t.Error("swagger missing Wallet definition")
	}

	// Group wallets list documents include=metrics and returns the single
	// GroupWallet $ref (SCAN-H-22).
	if !strings.Contains(string(raw), "internal_wallet.GroupWallet") {
		t.Error("swagger group wallets must reference the unified GroupWallet schema")
	}
	if grp, ok := spec.Paths["/api/v1/groups/{id}/wallets"]; !ok {
		t.Error("swagger missing GET /api/v1/groups/{id}/wallets")
	} else {
		grpParams := map[string]bool{}
		for _, p := range grp["get"].Parameters {
			grpParams[p.Name] = true
		}
		for _, want := range []string{"include", "timeframe", "sort", "order", "pnl_gt", "win_rate_gte"} {
			if !grpParams[want] {
				t.Errorf("swagger group wallets missing documented param %q", want)
			}
		}
	}
}
