package trader

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

// Spec v1.1 search periods. 90D exists only on the legacy API.
const (
	Period1D  = "1D"
	Period7D  = "7D"
	Period30D = "30D"
	PeriodALL = "ALL"
)

// DefaultVenue is the only venue shipping in V1; later DEX venues add enum
// values plus an adapter, no route changes (spec v1.1 multi-venue note).
const DefaultVenue = "hyperliquid"

// CurrentCalculationVersion stamps every period_metrics row. Bump on any
// metric-formula change so readers can tell estimates apart (spec v1.1 D4).
const CurrentCalculationVersion = 1

// Calculation v1 ROI fallback, used only when no fresh leaderboard window
// maps to the requested period (see LBWindowForPeriod). Formula (documented,
// never silent): roi_pct = 100 * period_pnl / period_volume; NULL when volume
// is missing or zero. Leaderboard passthrough always wins when available.
const ROIFallbackDoc = "roi=100*pnl/volume estimate v1; LB passthrough preferred"

// LBWindowForPeriod maps search periods to leaderboard windows for the ROI
// passthrough rule (spec v1.1 D4).
var LBWindowForPeriod = map[string]string{
	Period1D:  "day",
	Period7D:  "week",
	Period30D: "month",
	PeriodALL: "allTime",
}

// DataStatus is the per-row freshness exposed to FE (ready/syncing/stale/error).
type DataStatus string

const (
	DataReady   DataStatus = "ready"
	DataSyncing DataStatus = "syncing"
	DataStale   DataStatus = "stale"
	DataError   DataStatus = "error"
)

// DiscoverySource tracks how a registry row was found (spec §6, multi-venue:
// scoped per venue row).
type DiscoverySource string

const (
	SourceLeaderboard DiscoverySource = "leaderboard"
	SourceWSTrade     DiscoverySource = "ws_trade"
	SourceBoth        DiscoverySource = "both"
	SourceManual      DiscoverySource = "manual"
)

// RegistryEntry is one row of trader_registry: 1 wallet per venue.
type RegistryEntry struct {
	VenueID           uuid.UUID       `json:"venue_id"`
	Venue             string          `json:"venue"`
	WalletAddress     string          `json:"wallet_address"`
	FirstSeenAt       time.Time       `json:"first_seen_at"`
	LastSeenAt        time.Time       `json:"last_seen_at"`
	LastTradeAt       *time.Time      `json:"last_trade_at"`
	DiscoverySource   DiscoverySource `json:"discovery_source"`
	LeaderboardSeenAt *time.Time      `json:"leaderboard_seen_at"`
	DisplayName       *string         `json:"display_name"`
	Status            string          `json:"status"`
}

// SyncState is one row of trader_sync_state: incremental cursors + retries.
type SyncState struct {
	VenueID             uuid.UUID  `json:"venue_id"`
	WalletAddress       string     `json:"wallet_address"`
	FillsLastTime       *time.Time `json:"fills_last_time"`
	FillsLastTID        *int64     `json:"fills_last_tid"`
	LastFillsSyncAt     *time.Time `json:"last_fills_sync_at"`
	LastPortfolioSyncAt *time.Time `json:"last_portfolio_sync_at"`
	LastPositionsSyncAt *time.Time `json:"last_positions_sync_at"`
	BackfillStartTime   *time.Time `json:"backfill_start_time"`
	BackfillCompletedAt *time.Time `json:"backfill_completed_at"`
	SyncStatus          string     `json:"sync_status"`
	RetryCount          int        `json:"retry_count"`
	LastError           *string    `json:"last_error"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

// DailyStats is one row of trader_daily_stats: 1 wallet/day/venue.
type DailyStats struct {
	VenueID             uuid.UUID  `json:"venue_id"`
	WalletAddress       string     `json:"wallet_address"`
	StatDate            time.Time  `json:"stat_date"`
	TradeCount          int64      `json:"trade_count"`
	WinCount            int64      `json:"win_count"`
	LossCount           int64      `json:"loss_count"`
	BreakevenCount      int64      `json:"breakeven_count"`
	RealizedPnL         *float64   `json:"realized_pnl"`
	Fees                float64    `json:"fees"`
	Volume              *float64   `json:"volume"`
	GrossProfit         *float64   `json:"gross_profit"`
	GrossLoss           *float64   `json:"gross_loss"`
	LongCount           int64      `json:"long_count"`
	LongWins            int64      `json:"long_wins"`
	ShortCount          int64      `json:"short_count"`
	ShortWins           int64      `json:"short_wins"`
	HoldingTimeSecSum   float64    `json:"holding_time_sec_sum"`
	HoldingTimeSecCount int64      `json:"holding_time_sec_count"`
	LastTradeAt         *time.Time `json:"last_trade_at"`
}

// PeriodMetrics is one row of trader_period_metrics: 1 wallet/period/venue.
// It is the ONLY table the scanner reads (spec §14).
type PeriodMetrics struct {
	VenueID            uuid.UUID  `json:"venue_id"`
	Venue              string     `json:"venue"`
	WalletAddress      string     `json:"wallet_address"`
	DisplayName        *string    `json:"display_name"`
	Period             string     `json:"period"`
	AsOf               time.Time  `json:"metrics_as_of"`
	PnL                *float64   `json:"pnl"`
	RealizedPnL        *float64   `json:"realized_pnl"`
	ROI                *float64   `json:"roi"`
	WinRate            *float64   `json:"win_rate"`
	TradeCount         *int64     `json:"trade_count"`
	Volume             *float64   `json:"volume"`
	GrossProfit        *float64   `json:"gross_profit"`
	GrossLoss          *float64   `json:"gross_loss"`
	ProfitFactor       *float64   `json:"profit_factor"`
	AvgTradePnL        *float64   `json:"avg_trade_pnl"`
	LongCount          *int64     `json:"long_count"`
	LongWins           *int64     `json:"long_wins"`
	ShortCount         *int64     `json:"short_count"`
	ShortWins          *int64     `json:"short_wins"`
	MaxDrawdownPct     *float64   `json:"max_drawdown_pct"`
	AvgHoldingTimeSec  *float64   `json:"avg_holding_time_sec"`
	LastTradeAt        *time.Time `json:"last_trade_at"`
	DataStatus         DataStatus `json:"data_status"`
	IsPartial          bool       `json:"is_partial"`
	CalculationVersion int        `json:"calculation_version"`
}

// LeaderboardRef is one row of trader_leaderboard_ref: latest upstream window
// aggregates (3 numbers per window; raw payloads never stored).
type LeaderboardRef struct {
	VenueID       uuid.UUID `json:"venue_id"`
	WalletAddress string    `json:"wallet_address"`
	Window        string    `json:"window"`
	PnL           *float64  `json:"pnl"`
	ROI           *float64  `json:"roi"`
	Volume        *float64  `json:"volume"`
	AccountValue  *float64  `json:"account_value"`
	FetchedAt     time.Time `json:"fetched_at"`
}

var addressRe = regexp.MustCompile(`^0x[0-9a-f]{40}$`)

// NormalizeAddress lowercases and validates an EVM address (spec §6: normalize
// once at the boundary; DB CHECK enforces lowercase). Rejects safely.
func NormalizeAddress(raw string) (string, error) {
	addr := strings.ToLower(strings.TrimSpace(raw))
	if !addressRe.MatchString(addr) {
		return "", domain.NewError(domain.ErrCodeValidation, "invalid wallet address")
	}
	return addr, nil
}

// Sortable columns for sort_by: indexed metrics only (spec §13).
var sortableColumns = map[string]string{
	"pnl":         "pnl",
	"roi":         "roi",
	"win_rate":    "win_rate",
	"volume":      "volume",
	"trade_count": "trade_count",
	"last_trade":  "last_trade_at",
}

// SearchRequest is the POST /api/v1/traders/search body (spec §13 + v1.1 D7
// + CONTRACT.md 2026-10-06 numbered pagination).
//
// Numbered pagination vs keyset cursor (mutually exclusive, additive):
//   - Page == nil (absent) → legacy keyset path using Cursor (untouched).
//   - Page != nil (present) → offset path with offset = (page-1)*limit, and
//     any Cursor in the same body is IGNORED (precedence: page wins, so a
//     stale/forged cursor can never break a numbered-page request).
//
// EffectivePage defaults to 1 when Page is absent.
type SearchRequest struct {
	Period          string   `json:"period"`
	Venue           string   `json:"venue"`
	ROIMin          *float64 `json:"roi_min"`
	ROIMax          *float64 `json:"roi_max"`
	WinRateMin      *float64 `json:"win_rate_min"`
	WinRateMax      *float64 `json:"win_rate_max"`
	PnLMin          *float64 `json:"pnl_min"`
	PnLMax          *float64 `json:"pnl_max"`
	VolumeMin       *float64 `json:"volume_min"`
	VolumeMax       *float64 `json:"volume_max"`
	TradeCountMin   *int     `json:"trade_count_min"`
	TradeCountMax   *int     `json:"trade_count_max"`
	ProfitFactorMin *float64 `json:"profit_factor_min"`
	ProfitFactorMax *float64 `json:"profit_factor_max"`
	LongWinRateMin  *float64 `json:"long_win_rate_min"`
	LongWinRateMax  *float64 `json:"long_win_rate_max"`
	ShortWinRateMin *float64 `json:"short_win_rate_min"`
	ShortWinRateMax *float64 `json:"short_win_rate_max"`
	LastTradeAfter  string   `json:"last_trade_after"`
	GroupID         string   `json:"group_id"`
	SortBy          string   `json:"sort_by"`
	SortDirection   string   `json:"sort_direction"`
	Limit           int      `json:"limit"`
	Cursor          string   `json:"cursor"`
	Page            *int     `json:"page,omitempty"`
}

func invalidFilter(format string, args ...any) error {
	return domain.NewError(domain.ErrCodeInvalidFilter, fmt.Sprintf(format, args...))
}

// Normalize applies defaults (period 30D, venue hyperliquid, sort pnl desc,
// limit 50) and canonicalizes enums. Validate enforces every rule with
// INVALID_FILTER (spec BE-027/BE-039).
func (r *SearchRequest) Normalize() {
	r.Period = strings.ToUpper(strings.TrimSpace(r.Period))
	if r.Period == "" {
		r.Period = Period30D
	}
	r.Venue = strings.ToLower(strings.TrimSpace(r.Venue))
	if r.Venue == "" {
		r.Venue = DefaultVenue
	}
	r.SortBy = strings.ToLower(strings.TrimSpace(r.SortBy))
	if r.SortBy == "" {
		r.SortBy = "pnl"
	}
	r.SortDirection = strings.ToLower(strings.TrimSpace(r.SortDirection))
	if r.SortDirection == "" {
		r.SortDirection = "desc"
	}
	if r.Limit == 0 {
		r.Limit = 50
	}
	// Page is deliberately left untouched: nil (absent) selects the legacy
	// keyset path, non-nil selects the offset path. Defaulting nil to 1 here
	// would silently reroute cursor-walk continuations (Cursor set, Page
	// absent) into the offset path and break them; see EffectivePage.
}

// Validate checks the normalized request (call Normalize first).
func (r *SearchRequest) Validate() error {
	switch r.Period {
	case Period1D, Period7D, Period30D, PeriodALL:
	default:
		return invalidFilter("invalid period %q: want 1D|7D|30D|ALL", r.Period)
	}
	if r.Venue == "" {
		return invalidFilter("venue is required")
	}
	rangeF := func(name string, min, max *float64) error {
		if min != nil && max != nil && *min > *max {
			return invalidFilter("%s_min > %s_max", name, name)
		}
		return nil
	}
	for _, p := range []struct {
		name string
		min  *float64
		max  *float64
	}{{"roi", r.ROIMin, r.ROIMax}, {"pnl", r.PnLMin, r.PnLMax},
		{"volume", r.VolumeMin, r.VolumeMax}, {"profit_factor", r.ProfitFactorMin, r.ProfitFactorMax},
		{"long_win_rate", r.LongWinRateMin, r.LongWinRateMax},
		{"short_win_rate", r.ShortWinRateMin, r.ShortWinRateMax},
		{"win_rate", r.WinRateMin, r.WinRateMax}} {
		if err := rangeF(p.name, p.min, p.max); err != nil {
			return err
		}
	}
	if r.TradeCountMin != nil && r.TradeCountMax != nil && *r.TradeCountMin > *r.TradeCountMax {
		return invalidFilter("trade_count_min > trade_count_max")
	}
	pct := func(name string, v *float64) error {
		if v != nil && (*v < 0 || *v > 100) {
			return invalidFilter("%s out of range 0..100", name)
		}
		return nil
	}
	for _, p := range []struct {
		name string
		v    *float64
	}{{"win_rate_min", r.WinRateMin}, {"win_rate_max", r.WinRateMax},
		{"long_win_rate_min", r.LongWinRateMin}, {"long_win_rate_max", r.LongWinRateMax},
		{"short_win_rate_min", r.ShortWinRateMin}, {"short_win_rate_max", r.ShortWinRateMax}} {
		if err := pct(p.name, p.v); err != nil {
			return err
		}
	}
	nonNeg := func(name string, v *float64) error {
		if v != nil && *v < 0 {
			return invalidFilter("%s must be >= 0", name)
		}
		return nil
	}
	for _, p := range []struct {
		name string
		v    *float64
	}{{"volume_min", r.VolumeMin}, {"volume_max", r.VolumeMax},
		{"profit_factor_min", r.ProfitFactorMin}, {"profit_factor_max", r.ProfitFactorMax}} {
		if err := nonNeg(p.name, p.v); err != nil {
			return err
		}
	}
	if r.TradeCountMin != nil && *r.TradeCountMin < 0 {
		return invalidFilter("trade_count_min must be >= 0")
	}
	if r.TradeCountMax != nil && *r.TradeCountMax < 0 {
		return invalidFilter("trade_count_max must be >= 0")
	}
	if r.LastTradeAfter != "" {
		if _, err := time.Parse(time.RFC3339, r.LastTradeAfter); err != nil {
			return invalidFilter("last_trade_after must be ISO-8601 (RFC3339)")
		}
	}
	if r.GroupID != "" {
		if _, err := uuid.Parse(r.GroupID); err != nil {
			return invalidFilter("group_id must be a UUID")
		}
	}
	if _, ok := sortableColumns[r.SortBy]; !ok {
		return invalidFilter("invalid sort_by %q", r.SortBy)
	}
	if r.SortDirection != "asc" && r.SortDirection != "desc" {
		return invalidFilter("invalid sort_direction %q: want asc|desc", r.SortDirection)
	}
	if r.Limit < 1 || r.Limit > 100 {
		return invalidFilter("limit must be 1..100")
	}
	if r.Page != nil && *r.Page < 1 {
		return invalidFilter("page must be >= 1")
	}
	return nil
}

// UseOffset reports whether the numbered-pagination path applies: Page was
// explicitly present in the body (non-nil). The caller must then ignore
// Cursor entirely (precedence: page wins).
func (r *SearchRequest) UseOffset() bool { return r.Page != nil }

// EffectivePage returns the page number, defaulting to 1 when Page is
// absent (CONTRACT.md: default 1). Routing still uses presence (UseOffset),
// not this value, so absent keeps meaning the keyset path.
func (r *SearchRequest) EffectivePage() int {
	if r.Page == nil {
		return 1
	}
	return *r.Page
}

// TotalPages returns ceil(total/limit); 0 when there is nothing to page
// (total == 0) or the limit is invalid.
func TotalPages(total int64, limit int) int {
	if total <= 0 || limit <= 0 {
		return 0
	}
	return int((total + int64(limit) - 1) / int64(limit))
}

// Fingerprint binds a cursor to its query: any filter/sort change invalidates
// previously issued cursors (spec: stable snapshot per query).
//
// Page is intentionally NOT part of the fingerprint (CONTRACT.md §1): the
// offset path is stateless — it carries no sealed cursor, so there is nothing
// whose validity could depend on the page number. A filter/sort change
// between two numbered-page requests simply yields the new result set; no
// stale-cursor error is possible. The keyset path fingerprint below is
// unchanged, so old cursors keep validating exactly as before.
func (r *SearchRequest) Fingerprint() string {
	canonical, _ := json.Marshal(struct {
		Period, Venue, LastTradeAfter, GroupID, SortBy, SortDirection    string
		ROIMin, ROIMax, WinRateMin, WinRateMax, PnLMin, PnLMax           *float64
		VolumeMin, VolumeMax, ProfitFactorMin, ProfitFactorMax           *float64
		LongWinRateMin, LongWinRateMax, ShortWinRateMin, ShortWinRateMax *float64
		TradeCountMin, TradeCountMax                                     *int
		Limit                                                            int
	}{r.Period, r.Venue, r.LastTradeAfter, r.GroupID, r.SortBy, r.SortDirection,
		r.ROIMin, r.ROIMax, r.WinRateMin, r.WinRateMax, r.PnLMin, r.PnLMax,
		r.VolumeMin, r.VolumeMax, r.ProfitFactorMin, r.ProfitFactorMax,
		r.LongWinRateMin, r.LongWinRateMax, r.ShortWinRateMin, r.ShortWinRateMax,
		r.TradeCountMin, r.TradeCountMax, r.Limit})
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])
}

type pageCursor struct {
	FP string `json:"fp"`
	M  string `json:"m"` // last sort value; "" = NULL
	A  string `json:"a"` // last wallet address (tiebreak)
}

// EncodeCursor seals an opaque page cursor: base64(payload).base64(HMAC).
// sortValue nil encodes as "" (NULL).
func EncodeCursor(secret []byte, fp string, sortValue *string, addr string) (string, error) {
	m := ""
	if sortValue != nil {
		m = *sortValue
	}
	raw, err := json.Marshal(pageCursor{FP: fp, M: m, A: addr})
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(raw)
	sig := mac.Sum(nil)
	return base64.RawURLEncoding.EncodeToString(raw) + "." +
		base64.RawURLEncoding.EncodeToString(sig), nil
}

// DecodeCursor verifies and opens a cursor bound to fp. Tampered, foreign or
// malformed cursors are INVALID_FILTER (spec BE-026/BE-027).
func DecodeCursor(secret []byte, fp, raw string) (sortValue *string, addr string, err error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 2 {
		return nil, "", invalidFilter("malformed cursor")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, "", invalidFilter("malformed cursor")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, "", invalidFilter("malformed cursor")
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(payload)
	if !hmac.Equal(mac.Sum(nil), sig) {
		return nil, "", invalidFilter("invalid cursor signature")
	}
	var c pageCursor
	if err := json.Unmarshal(payload, &c); err != nil {
		return nil, "", invalidFilter("malformed cursor")
	}
	if c.FP != fp {
		return nil, "", invalidFilter("cursor does not match this query")
	}
	if c.A == "" {
		return nil, "", invalidFilter("malformed cursor")
	}
	if c.M == "" {
		return nil, c.A, nil
	}
	m := c.M
	return &m, c.A, nil
}
