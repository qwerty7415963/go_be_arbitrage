package wallet

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

type FilterOp string

const (
	OpGT      FilterOp = "gt"
	OpGTE     FilterOp = "gte"
	OpLT      FilterOp = "lt"
	OpLTE     FilterOp = "lte"
	OpBetween FilterOp = "between"
)

type NumericFilter struct {
	Metric string
	Op     FilterOp
	Lo     float64
	Hi     *float64 // set only for between
}

type Filters struct {
	Search         string
	Dex            []string
	Chain          []string
	Market         []string
	Timeframe      string // 24H|7D|30D|90D|ALL (default 30D) — custom sets CustomKey
	Start          *time.Time
	End            *time.Time
	CustomKey      string // snapshot key for custom ranges
	Numeric        []NumericFilter
	LastActiveFrom *time.Time
	LastActiveTo   *time.Time
}

type SortSpec struct {
	Field string // pnl|roi|win_rate|volume|trade_count|avg_position|avg_leverage|last_active
	Order string // asc|desc
}

// numericSpec declares which metrics accept numeric filters and their
// allowed range (nil bound = unbounded). Negative values are rejected
// wherever the metric cannot be negative (SCAN-U-05).
type numericSpec struct {
	min *float64
	max *float64
}

func fptr(v float64) *float64 { return &v }

var numericMetrics = map[string]numericSpec{
	"pnl":              {min: nil, max: nil}, // realized PnL can be negative
	"roi":              {min: nil, max: nil},
	"win_rate":         {min: fptr(0), max: fptr(100)},
	"volume":           {min: fptr(0), max: nil},
	"trade_count":      {min: fptr(0), max: nil},
	"avg_position":     {min: fptr(0), max: nil},
	"avg_leverage":     {min: fptr(0), max: nil},
	"long_short_ratio": {min: fptr(0), max: nil},
}

var validSuffixes = map[string]FilterOp{
	"_gt":      OpGT,
	"_gte":     OpGTE,
	"_lt":      OpLT,
	"_lte":     OpLTE,
	"_between": OpBetween,
}

var sortableFields = map[string]bool{
	"pnl": true, "roi": true, "win_rate": true, "volume": true,
	"trade_count": true, "avg_position": true, "avg_leverage": true,
	"last_active": true,
}

func validationError(field, msg string) error {
	return domain.NewError(domain.ErrCodeValidation, "validation failed").
		WithDetails([]map[string]string{{"field": field, "code": string(domain.ErrCodeValidation), "message": msg}})
}

// multiSelect collects repeated and comma-separated values, normalized to
// lowercase/trimmed, and validates each against the configured enum.
func multiSelect(q url.Values, key string, allowed []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, raw := range q[key] {
		for _, part := range strings.Split(raw, ",") {
			v := strings.ToLower(strings.TrimSpace(part))
			if v == "" {
				continue
			}
			// Empty enum sets (fresh DB, no observed values yet) disable
			// validation instead of rejecting every filter value.
			if len(allowed) > 0 && !containsFold(allowed, v) {
				return nil, validationError(key, fmt.Sprintf("unknown %s %q", key, v))
			}
			if !seen[v] {
				seen[v] = true
				out = append(out, v)
			}
		}
	}
	return out, nil
}

func containsFold(list []string, v string) bool {
	for _, item := range list {
		if strings.EqualFold(item, v) {
			return true
		}
	}
	return false
}

func parseTimeParam(q url.Values, key string) (*time.Time, error) {
	raw := strings.TrimSpace(q.Get(key))
	if raw == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, validationError(key, "must be an RFC3339 timestamp")
	}
	return &t, nil
}

// ParseFilters parses and validates the scanner query grammar (TEST-01).
// Unknown enum values, bad operators, reversed ranges and out-of-range
// values are COMMON-902 errors — never silently coerced.
func ParseFilters(q url.Values, cfg FilterConfig) (*Filters, error) {
	f := &Filters{Timeframe: Timeframe30D}

	f.Search = strings.TrimSpace(q.Get("search")) // empty/whitespace = no search (SCAN-U-07)

	dex, err := multiSelect(q, "dex", cfg.Dexes)
	if err != nil {
		return nil, err
	}
	f.Dex = dex

	chain, err := multiSelect(q, "chain", cfg.Chains)
	if err != nil {
		return nil, err
	}
	f.Chain = chain

	market, err := multiSelect(q, "market", cfg.Markets)
	if err != nil {
		return nil, err
	}
	f.Market = market

	// Custom range and named timeframe are mutually exclusive.
	start, err := parseTimeParam(q, "start")
	if err != nil {
		return nil, err
	}
	end, err := parseTimeParam(q, "end")
	if err != nil {
		return nil, err
	}
	if start != nil || end != nil {
		if start == nil || end == nil {
			return nil, validationError("start", "custom range requires both start and end")
		}
		if !start.Before(*end) { // SCAN-U-08: start >= end rejected
			return nil, validationError("start", "start must be before end")
		}
		s, e := start.UTC(), end.UTC()
		f.Start, f.End = &s, &e
		f.CustomKey = fmt.Sprintf("%s:%s..%s", TimeframeCustom,
			s.Format(time.RFC3339), e.Format(time.RFC3339))
	} else {
		tf := strings.ToUpper(strings.TrimSpace(q.Get("timeframe")))
		if tf == "" {
			tf = Timeframe30D
		}
		if !ValidTimeframe(tf) { // SCAN-U-02
			return nil, validationError("timeframe", fmt.Sprintf("unknown timeframe %q", tf))
		}
		f.Timeframe = tf
	}

	// Numeric metric filters: <metric>_<gt|gte|lt|lte|between>=value
	for key, values := range q {
		metric, op, matched, badSuffix := matchNumericKey(key)
		if badSuffix {
			return nil, validationError(key, "unknown filter operator")
		}
		if !matched {
			continue
		}
		spec := numericMetrics[metric]
		for _, raw := range values {
			nf, err := parseNumeric(metric, op, raw, spec)
			if err != nil {
				return nil, err
			}
			f.Numeric = append(f.Numeric, *nf)
		}
	}

	// last_active relative or explicit range
	if within := strings.TrimSpace(q.Get("last_active_within")); within != "" {
		d, err := time.ParseDuration(within)
		if err != nil || d <= 0 {
			return nil, validationError("last_active_within", "must be a positive duration like 24h")
		}
		f.LastActiveFrom = timePtr(time.Now().UTC().Add(-d))
	}
	from, err := parseTimeParam(q, "last_active_from")
	if err != nil {
		return nil, err
	}
	to, err := parseTimeParam(q, "last_active_to")
	if err != nil {
		return nil, err
	}
	if from != nil && to != nil && !from.Before(*to) {
		return nil, validationError("last_active_from", "from must be before to")
	}
	f.LastActiveFrom, f.LastActiveTo = from, to

	return f, nil
}

func timePtr(t time.Time) *time.Time { return &t }

// matchNumericKey reports whether key targets a numeric metric filter:
// matched=false for unrelated query params, badSuffix=true when the metric
// is known but the operator suffix is invalid (SCAN-H-11 → COMMON-902).
func matchNumericKey(key string) (metric string, op FilterOp, matched, badSuffix bool) {
	for m := range numericMetrics {
		if !strings.HasPrefix(key, m+"_") {
			continue
		}
		suffix := key[len(m):]
		o, ok := validSuffixes[suffix]
		if !ok {
			return m, "", false, true
		}
		return m, o, true, false
	}
	return "", "", false, false
}

func parseNumeric(metric string, op FilterOp, raw string, spec numericSpec) (*NumericFilter, error) {
	lo, hi, err := parseNumericValues(op, raw)
	if err != nil {
		return nil, validationError(metric, err.Error())
	}
	for _, v := range append([]float64{lo}, hiOrEmpty(hi)...) {
		if spec.min != nil && v < *spec.min {
			return nil, validationError(metric, fmt.Sprintf("%s must be >= %g", metric, *spec.min))
		}
		if spec.max != nil && v > *spec.max {
			return nil, validationError(metric, fmt.Sprintf("%s must be <= %g", metric, *spec.max))
		}
	}
	return &NumericFilter{Metric: metric, Op: op, Lo: lo, Hi: hi}, nil
}

func hiOrEmpty(hi *float64) []float64 {
	if hi == nil {
		return nil
	}
	return []float64{*hi}
}

func parseNumericValues(op FilterOp, raw string) (float64, *float64, error) {
	if op == OpBetween {
		parts := strings.Split(raw, ",")
		if len(parts) != 2 {
			return 0, nil, fmt.Errorf("between requires two comma-separated numbers")
		}
		lo, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
		if err != nil {
			return 0, nil, fmt.Errorf("between lower bound is not a number")
		}
		hi, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
		if err != nil {
			return 0, nil, fmt.Errorf("between upper bound is not a number")
		}
		if lo > hi { // SCAN-U-04: reversed range rejected
			return 0, nil, fmt.Errorf("between requires lower <= upper")
		}
		return lo, &hi, nil
	}

	v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return 0, nil, fmt.Errorf("value is not a number")
	}
	return v, nil, nil
}

// ParseSort parses sort/order (default pnl desc — BR-12); invalid fields or
// orders are COMMON-902 (SCAN-U-10, SCAN-H-11).
func ParseSort(q url.Values) (*SortSpec, error) {
	field := strings.ToLower(strings.TrimSpace(q.Get("sort")))
	if field == "" {
		field = "pnl"
	}
	if !sortableFields[field] {
		return nil, validationError("sort", fmt.Sprintf("unknown sort field %q", field))
	}
	order := strings.ToLower(strings.TrimSpace(q.Get("order")))
	if order == "" {
		order = "desc"
	}
	if order != "asc" && order != "desc" {
		return nil, validationError("order", fmt.Sprintf("unknown order %q", order))
	}
	return &SortSpec{Field: field, Order: order}, nil
}

// ParsePageLimit clamps pagination inputs (default 50, max 200).
func ParsePageLimit(q url.Values) (page, limit int) {
	page, _ = strconv.Atoi(strings.TrimSpace(q.Get("page")))
	limit, _ = strconv.Atoi(strings.TrimSpace(q.Get("limit")))
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	return page, limit
}
