package wallet

import "testing"

// CFG-U-01: the config payload mirrors the parser tables exactly — every
// filterable metric with its range/ops, every sortable field, every
// timeframe — so the frontend can never drift from what the API accepts.
func TestBuildScannerConfig_MirrorsParserTables(t *testing.T) {
	cfg := buildScannerConfig(testConfig())

	if cfg.DefaultTimeframe != Timeframe30D {
		t.Errorf("default timeframe: got %q", cfg.DefaultTimeframe)
	}
	if cfg.DefaultSort != "pnl" {
		t.Errorf("default sort: got %q", cfg.DefaultSort)
	}

	// Timeframes: exactly the valid set.
	if len(cfg.Timeframes) != len(validTimeframes) {
		t.Fatalf("timeframes: got %v", cfg.Timeframes)
	}
	for tf := range validTimeframes {
		found := false
		for _, got := range cfg.Timeframes {
			if got == tf {
				found = true
			}
		}
		if !found {
			t.Errorf("timeframes missing %q", tf)
		}
	}

	// Sort fields: exactly the sortable set.
	if len(cfg.SortFields) != len(sortableFields) {
		t.Errorf("sort_fields: got %v", cfg.SortFields)
	}
	for sf := range sortableFields {
		found := false
		for _, got := range cfg.SortFields {
			if got == sf {
				found = true
			}
		}
		if !found {
			t.Errorf("sort_fields missing %q", sf)
		}
	}

	// Metrics: one entry per numericMetrics key with matching range + ops.
	if len(cfg.Metrics) != len(numericMetrics) {
		t.Fatalf("metrics: got %d, want %d", len(cfg.Metrics), len(numericMetrics))
	}
	for _, m := range cfg.Metrics {
		spec, ok := numericMetrics[m.Key]
		if !ok {
			t.Errorf("unknown metric %q", m.Key)
			continue
		}
		if m.Min != spec.min || m.Max != spec.max {
			t.Errorf("%s range: got [%v,%v], want [%v,%v]", m.Key, m.Min, m.Max, spec.min, spec.max)
		}
		if len(m.Ops) != len(opOrder) {
			t.Errorf("%s ops: got %v", m.Key, m.Ops)
		}
		if m.Sortable != sortableFields[m.Key] {
			t.Errorf("%s sortable: got %v", m.Key, m.Sortable)
		}
	}

	// DB-driven enums pass through (empty slices never null).
	if len(cfg.Dexes) == 0 || len(cfg.Chains) == 0 || len(cfg.Markets) == 0 {
		t.Errorf("enums empty: %+v", cfg)
	}
	if cfg.Operators == nil {
		t.Error("operators must not be null")
	}
}
