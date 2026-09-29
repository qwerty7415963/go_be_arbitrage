package wallet

import "sort"

// ScannerConfig is the GET /wallets/filter-config payload (CFG-*): the
// data-driven enums plus the code-declared metric/timeframe/sort tables,
// so the frontend renders filter controls dynamically instead of
// hard-coding them.
type ScannerConfig struct {
	Dexes            []string       `json:"dexes"`
	Chains           []string       `json:"chains"`
	Markets          []string       `json:"markets"`
	Timeframes       []string       `json:"timeframes"`
	DefaultTimeframe string         `json:"default_timeframe"`
	SortFields       []string       `json:"sort_fields"`
	DefaultSort      string         `json:"default_sort"`
	Operators        []string       `json:"operators"`
	Metrics          []MetricConfig `json:"metrics"`
}

// MetricConfig describes one filterable metric: allowed range (nil bound =
// unbounded, mirroring numericSpec), the accepted operators and whether
// the metric is sortable.
type MetricConfig struct {
	Key      string   `json:"key"`
	Min      *float64 `json:"min"`
	Max      *float64 `json:"max"`
	Ops      []string `json:"ops"`
	Sortable bool     `json:"sortable"`
}

// opOrder is the canonical operator order reported to clients.
var opOrder = []string{"gt", "gte", "lt", "lte", "between"}

// buildScannerConfig assembles the response from FilterConfig (DB enums)
// and the parser tables (numericMetrics / sortableFields /
// validTimeframes). Keys are sorted so the payload is deterministic
// (CFG-U-01) — the response must mirror the parser exactly.
func buildScannerConfig(cfg FilterConfig) *ScannerConfig {
	timeframes := make([]string, 0, len(validTimeframes))
	for tf := range validTimeframes {
		timeframes = append(timeframes, tf)
	}
	sort.Strings(timeframes)

	sortFields := make([]string, 0, len(sortableFields))
	for sf := range sortableFields {
		sortFields = append(sortFields, sf)
	}
	sort.Strings(sortFields)

	keys := make([]string, 0, len(numericMetrics))
	for k := range numericMetrics {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	metrics := make([]MetricConfig, 0, len(keys))
	for _, k := range keys {
		spec := numericMetrics[k]
		metrics = append(metrics, MetricConfig{
			Key:      k,
			Min:      spec.min,
			Max:      spec.max,
			Ops:      opOrder,
			Sortable: sortableFields[k],
		})
	}

	return &ScannerConfig{
		Dexes:            nonNil(cfg.Dexes),
		Chains:           nonNil(cfg.Chains),
		Markets:          nonNil(cfg.Markets),
		Timeframes:       timeframes,
		DefaultTimeframe: Timeframe30D,
		SortFields:       sortFields,
		DefaultSort:      "pnl",
		Operators:        opOrder,
		Metrics:          metrics,
	}
}
