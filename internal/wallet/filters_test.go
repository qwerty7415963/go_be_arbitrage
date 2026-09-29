package wallet

import (
	"errors"
	"net/url"
	"testing"

	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

func testConfig() FilterConfig {
	return FilterConfig{
		Dexes:   []string{"hyperliquid", "gmx", "extended"},
		Chains:  []string{"evm", "starknet"},
		Markets: []string{"BTC", "ETH"},
	}
}

func parseErr(t *testing.T, q url.Values) *domain.AppError {
	t.Helper()
	_, err := ParseFilters(q, testConfig())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var appErr *domain.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected AppError, got %T: %v", err, err)
	}
	return appErr
}

// SCAN-U-01: valid enum values parse cleanly.
func TestParseFilters_ValidEnums_Parsed(t *testing.T) {
	q := url.Values{"dex": {"hyperliquid"}, "chain": {"starknet"}, "timeframe": {"7d"}}
	f, err := ParseFilters(q, testConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.Dex[0] != "hyperliquid" || f.Chain[0] != "starknet" {
		t.Errorf("dex/chain not parsed: %+v", f)
	}
	if f.Timeframe != Timeframe7D {
		t.Errorf("expected timeframe 7D, got %q", f.Timeframe)
	}
}

// SCAN-U-02: unknown dex / chain / timeframe are COMMON-902, never coerced.
func TestParseFilters_UnknownEnum_ReturnsCommon902(t *testing.T) {
	for name, q := range map[string]url.Values{
		"dex":       {"dex": {"not_a_dex"}},
		"chain":     {"chain": {"bitcoin"}},
		"timeframe": {"timeframe": {"1Y"}},
	} {
		if appErr := parseErr(t, q); appErr.Code != domain.ErrCodeValidation {
			t.Errorf("%s: expected COMMON-902, got %s", name, appErr.Code)
		}
	}
}

// SCAN-U-03: gt/gte/lt/lte operators parse with the right op.
func TestParseFilters_NumericOps_Parsed(t *testing.T) {
	q := url.Values{
		"pnl_gt":     {"100"},
		"pnl_lte":    {"1000.5"},
		"volume_gte": {"0"},
		"roi_lt":     {"-10"},
	}
	f, err := ParseFilters(q, testConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.Numeric) != 4 {
		t.Fatalf("expected 4 filters, got %d", len(f.Numeric))
	}
	ops := map[FilterOp]bool{}
	metrics := map[string]bool{}
	for _, nf := range f.Numeric {
		ops[nf.Op] = true
		metrics[nf.Metric] = true
	}
	for _, op := range []FilterOp{OpGT, OpLTE, OpGTE, OpLT} {
		if !ops[op] {
			t.Errorf("missing op %s", op)
		}
	}
	if !metrics["pnl"] || !metrics["volume"] || !metrics["roi"] {
		t.Errorf("missing metrics: %v", metrics)
	}
}

// SCAN-U-04: between with reversed bounds is rejected (BE-03).
func TestParseFilters_BetweenReversed_Rejects(t *testing.T) {
	if appErr := parseErr(t, url.Values{"pnl_between": {"10,5"}}); appErr.Code != domain.ErrCodeValidation {
		t.Errorf("expected COMMON-902, got %s", appErr.Code)
	}
}

// SCAN-U-05: non-numeric values and disallowed negatives reject without
// silent coercion.
func TestParseFilters_BadNumeric_Rejects(t *testing.T) {
	for name, q := range map[string]url.Values{
		"between non-numeric": {"pnl_between": {"abc,5"}},
		"negative volume":     {"volume_gt": {"-1"}},
		"win_rate over 100":   {"win_rate_gt": {"101"}},
		"non-numeric scalar":  {"roi_gte": {"12abc"}},
	} {
		if appErr := parseErr(t, q); appErr.Code != domain.ErrCodeValidation {
			t.Errorf("%s: expected COMMON-902, got %s", name, appErr.Code)
		}
	}
}

// SCAN-U-06: decimal and boundary values parse exactly.
func TestParseFilters_BoundaryValues_ParsedExactly(t *testing.T) {
	q := url.Values{
		"volume_gte":     {"0"},          // min boundary allowed
		"win_rate_lte":   {"100"},        // max boundary allowed
		"pnl_gte":        {"-1234.5678"}, // negative pnl allowed
		"trade_count_gt": {"2.5"},
	}
	f, err := ParseFilters(q, testConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.Numeric) != 4 {
		t.Fatalf("expected 4 filters, got %d", len(f.Numeric))
	}
	for _, nf := range f.Numeric {
		if nf.Metric == "pnl" && nf.Lo != -1234.5678 {
			t.Errorf("expected exact -1234.5678, got %v", nf.Lo)
		}
		if nf.Metric == "win_rate" && nf.Lo != 100 {
			t.Errorf("expected exact 100, got %v", nf.Lo)
		}
	}
}

// SCAN-U-07: empty / whitespace search = no search term, no error.
func TestParseFilters_BlankSearch_NoSearchTerm(t *testing.T) {
	for _, raw := range []string{"", "   "} {
		f, err := ParseFilters(url.Values{"search": {raw}}, testConfig())
		if err != nil {
			t.Fatalf("search %q: unexpected error: %v", raw, err)
		}
		if f.Search != "" {
			t.Errorf("search %q: expected empty search, got %q", raw, f.Search)
		}
	}
}

// SCAN-U-08: custom range start >= end (or half-open) rejected (BE-04).
func TestParseFilters_CustomRangeInvalid_Rejects(t *testing.T) {
	for name, q := range map[string]url.Values{
		"start equals end": {
			"start": {"2026-01-01T00:00:00Z"},
			"end":   {"2026-01-01T00:00:00Z"},
		},
		"start after end": {
			"start": {"2026-02-01T00:00:00Z"},
			"end":   {"2026-01-01T00:00:00Z"},
		},
		"missing end": {"start": {"2026-01-01T00:00:00Z"}},
		"bad rfc3339": {"start": {"not-a-time"}, "end": {"2026-01-01T00:00:00Z"}},
	} {
		if appErr := parseErr(t, q); appErr.Code != domain.ErrCodeValidation {
			t.Errorf("%s: expected COMMON-902, got %s", name, appErr.Code)
		}
	}
}

// SCAN-U-09: valid sort field + order parse (default handled separately).
func TestParseSort_Valid_Parsed(t *testing.T) {
	s, err := ParseSort(url.Values{"sort": {"volume"}, "order": {"asc"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Field != "volume" || s.Order != "asc" {
		t.Errorf("got %+v", s)
	}

	s, err = ParseSort(url.Values{})
	if err != nil || s.Field != "pnl" || s.Order != "desc" {
		t.Errorf("default: got %+v, err %v", s, err)
	}
}

// SCAN-U-10: invalid sort field / order are COMMON-902 (BE-05).
func TestParseSort_Invalid_ReturnsCommon902(t *testing.T) {
	for name, q := range map[string]url.Values{
		"field": {"sort": {"sharpe"}},
		"order": {"order": {"sideways"}},
	} {
		_, err := ParseSort(q)
		var appErr *domain.AppError
		if !errors.As(err, &appErr) || appErr.Code != domain.ErrCodeValidation {
			t.Errorf("%s: expected COMMON-902, got %v", name, err)
		}
	}
}

// SCAN-U-11: multi-select — repeated and comma forms, deduped (BR-11 OR
// semantics are completed by the SQL layer).
func TestParseFilters_MultiSelect_RepeatedAndComma(t *testing.T) {
	q := url.Values{"dex": {"hyperliquid,gmx", "hyperliquid"}}
	f, err := ParseFilters(q, testConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.Dex) != 2 {
		t.Fatalf("expected 2 deduped dex values, got %v", f.Dex)
	}
	if f.Dex[0] != "hyperliquid" || f.Dex[1] != "gmx" {
		t.Errorf("got %v", f.Dex)
	}
}

// SCAN-U-12: 2+ metric filters are all collected (AND semantics completed
// by the SQL layer).
func TestParseFilters_MultipleMetrics_AllCollected(t *testing.T) {
	q := url.Values{"pnl_gt": {"100"}, "win_rate_gte": {"60"}}
	f, err := ParseFilters(q, testConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.Numeric) != 2 {
		t.Fatalf("expected 2 filters, got %d", len(f.Numeric))
	}
	if f.Numeric[0].Metric == f.Numeric[1].Metric {
		t.Errorf("expected distinct metrics, got %v", f.Numeric)
	}
}

// SCAN-U-21: every metric supports every operator suffix (symmetric ops).
func TestParseFilters_AllMetricsAllOps_Parsed(t *testing.T) {
	metrics := []string{"pnl", "roi", "win_rate", "volume", "trade_count",
		"avg_position", "avg_leverage", "long_short_ratio"}
	suffixes := map[string]FilterOp{
		"gt": OpGT, "gte": OpGTE, "lt": OpLT, "lte": OpLTE, "between": OpBetween,
	}
	for _, m := range metrics {
		for suffix, wantOp := range suffixes {
			raw := "5"
			if wantOp == OpBetween {
				raw = "1,10"
			}
			// win_rate is capped at 100; keep values in range.
			if m == "win_rate" && wantOp == OpBetween {
				raw = "10,90"
			}
			f, err := ParseFilters(url.Values{m + "_" + suffix: {raw}}, testConfig())
			if err != nil {
				t.Fatalf("%s_%s: unexpected error: %v", m, suffix, err)
			}
			if len(f.Numeric) != 1 {
				t.Fatalf("%s_%s: expected 1 filter, got %d", m, suffix, len(f.Numeric))
			}
			nf := f.Numeric[0]
			if nf.Metric != m || nf.Op != wantOp {
				t.Errorf("%s_%s: got %+v", m, suffix, nf)
			}
			if wantOp == OpBetween && (nf.Hi == nil || nf.Lo >= *nf.Hi) {
				t.Errorf("%s_between: bad bounds: %+v", m, nf)
			}
		}
	}
}

// Unknown operator suffix on a known metric is COMMON-902: the parser never
// ignores an invalid operator silently.
func TestParseFilters_UnknownOperator_ReturnsCommon902(t *testing.T) {
	if appErr := parseErr(t, url.Values{"pnl_approx": {"1"}}); appErr.Code != domain.ErrCodeValidation {
		t.Errorf("expected COMMON-902, got %s", appErr.Code)
	}
}

// WL-U-01: watchlisted=true/false sets the flag; absent leaves it nil.
func TestParseFilters_Watchlisted_Parsed(t *testing.T) {
	f, err := ParseFilters(url.Values{"watchlisted": {"true"}}, testConfig())
	if err != nil {
		t.Fatalf("watchlisted=true: %v", err)
	}
	if f.Watchlisted == nil || !*f.Watchlisted {
		t.Errorf("expected true, got %v", f.Watchlisted)
	}

	f, err = ParseFilters(url.Values{"watchlisted": {"FALSE"}}, testConfig())
	if err != nil {
		t.Fatalf("watchlisted=false: %v", err)
	}
	if f.Watchlisted == nil || *f.Watchlisted {
		t.Errorf("expected false, got %v", f.Watchlisted)
	}

	f, err = ParseFilters(url.Values{}, testConfig())
	if err != nil {
		t.Fatalf("absent: %v", err)
	}
	if f.Watchlisted != nil {
		t.Errorf("absent must stay nil (no filter), got %v", *f.Watchlisted)
	}
}

// WL-U-02: non-boolean watchlisted value is COMMON-902.
func TestParseFilters_Watchlisted_InvalidCommon902(t *testing.T) {
	if appErr := parseErr(t, url.Values{"watchlisted": {"maybe"}}); appErr.Code != domain.ErrCodeValidation {
		t.Errorf("expected COMMON-902, got %s", appErr.Code)
	}
}
