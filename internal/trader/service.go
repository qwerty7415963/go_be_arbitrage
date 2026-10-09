package trader

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

// errUpstreamUnavailable signals a nil on-demand client (unit contexts
// without a venue client): callers degrade to stale cache or empty payloads
// with data_status=error (never 500).
var errUpstreamUnavailable = errors.New("upstream unavailable")

// GroupOwner resolves a search group_id's owner (*tradergroup.Repository
// implements it; declared here so trader never imports the groups package).
type GroupOwner interface {
	OwnerOf(ctx context.Context, groupID uuid.UUID) (uuid.UUID, error)
}

// Service serves the scanner reads (search/detail, DB) + the LIVE wallet
// detail tabs (positions/activity/balances/fills/orders/transfers/performance,
// LIVE-CONTRACT v1.2 §1.1-§1.7, no sync dependency). Search/detail are public
// (parity with the legacy scanner); the group filter and trader-groups stay
// auth-gated. WS-E: syncing/stale machinery removed; detail data_status ∈
// {ready,error} ONLY.
type Service struct {
	repo   *Repository
	groups GroupOwner
	secret []byte
	// onDemand serves the LIVE wallet tabs behind the interactive client (own
	// pacer, concurrency gate, ctx timeout) + TTL cache + single-flight. Nil
	// disables HL reads (unit contexts without a venue client); handlers then
	// surface data_status=error via degradation (never 500).
	onDemand OnDemandClient
	cache    *OnDemandCache
}

func NewService(repo *Repository, groups GroupOwner, cursorSecret []byte) *Service {
	return &Service{repo: repo, groups: groups, secret: cursorSecret,
		cache: NewOnDemandCache(LiveCacheTTL)}
}

// WithOnDemand attaches the venue on-demand client + cache (LIVE §1.1-§1.7).
// Nil client disables HL reads (service returns graceful-degradation payloads).
func (s *Service) WithOnDemand(c OnDemandClient, cache *OnDemandCache) *Service {
	s.onDemand = c
	if cache != nil {
		s.cache = cache
	}
	return s
}

// SearchResult is one page: rows + opaque cursor for the next page (keyset
// path) or page/total/totalPages for the numbered path (CONTRACT.md
// 2026-10-06). Both paths always carry Total/TotalPages; the keyset path
// additionally carries NextCursor/HasMore as before, the offset path carries
// Page and derives HasMore from page < totalPages (NextCursor empty:
// the offset path is stateless, no cursor to continue from).
type SearchResult struct {
	Rows       []*PeriodMetrics `json:"rows"`
	NextCursor string           `json:"next_cursor,omitempty"`
	HasMore    bool             `json:"has_more"`
	Page       int              `json:"page,omitempty"`
	Total      int64            `json:"total,omitempty"`
	TotalPages int              `json:"total_pages,omitempty"`
}

// Search validates, resolves venue/group, then serves either the legacy
// keyset path (Page absent, spec §14) or the numbered offset path (Page
// present: offset = (page-1)*limit, Cursor ignored). Both read
// period_metrics only (never upstream, spec §14).
func (s *Service) Search(ctx context.Context, userID uuid.UUID, req *SearchRequest) (*SearchResult, error) {
	req.Normalize()
	if err := req.Validate(); err != nil {
		return nil, err
	}
	venueID, err := s.repo.VenueIDByCode(ctx, req.Venue)
	if err != nil {
		return nil, domain.NewError(domain.ErrCodeInvalidFilter, "unknown venue: "+req.Venue)
	}
	var groupID *uuid.UUID
	if req.GroupID != "" {
		gid, _ := uuid.Parse(req.GroupID)
		owner, err := s.groups.OwnerOf(ctx, gid)
		if err != nil {
			return nil, err
		}
		if userID == uuid.Nil {
			return nil, domain.NewError(domain.ErrCodeAuthTokenInvalid,
				"group filter requires authentication")
		}
		if owner != userID {
			return nil, domain.NewError(domain.ErrCodeGroupForbidden, "not your group")
		}
		groupID = &gid
	}
	if req.UseOffset() {
		return s.searchOffset(ctx, req, venueID, groupID)
	}
	return s.searchKeyset(ctx, req, venueID, groupID)
}

// searchOffset serves the numbered-pagination path: COUNT(*) with identical
// filters for the total, then the exact LIMIT/OFFSET window. page >
// totalPages (and total > 0) yields empty data with has_more=false.
func (s *Service) searchOffset(ctx context.Context, req *SearchRequest, venueID uuid.UUID, groupID *uuid.UUID) (*SearchResult, error) {
	total, err := s.countTotal(ctx, req, venueID, groupID)
	if err != nil {
		return nil, err
	}
	page := req.EffectivePage()
	totalPages := TotalPages(total, req.Limit)
	res := &SearchResult{Rows: []*PeriodMetrics{}, Page: page, Total: total, TotalPages: totalPages}
	if total > 0 && page > totalPages {
		return res, nil
	}
	query, args := buildSearchOffsetQuery(req, venueID, groupID, (page-1)*req.Limit)
	rows, err := s.repo.searchRaw(ctx, query, args)
	if err != nil {
		return nil, err
	}
	res.Rows = rows
	res.HasMore = int64((page-1)*req.Limit+len(rows)) < total
	return res, nil
}

// searchKeyset serves the legacy cursor path (spec BE-024/025/026), unchanged
// apart from additionally reporting Total/TotalPages for the shared meta.
func (s *Service) searchKeyset(ctx context.Context, req *SearchRequest, venueID uuid.UUID, groupID *uuid.UUID) (*SearchResult, error) {
	fp := req.Fingerprint()
	var curVal *string
	var curAddr string
	if req.Cursor != "" {
		v, a, err := DecodeCursor(s.secret, fp, req.Cursor)
		if err != nil {
			return nil, err
		}
		curVal, curAddr = v, a
	}
	query, args := buildSearchQuery(req, venueID, groupID, curVal, curAddr)
	rows, err := s.repo.searchRaw(ctx, query, args)
	if err != nil {
		return nil, err
	}
	total, err := s.countTotal(ctx, req, venueID, groupID)
	if err != nil {
		return nil, err
	}
	res := &SearchResult{Rows: []*PeriodMetrics{}, Total: total, TotalPages: TotalPages(total, req.Limit)}
	if len(rows) > req.Limit {
		res.HasMore = true
		rows = rows[:req.Limit]
	}
	res.Rows = rows
	if res.HasMore {
		last := rows[len(rows)-1]
		var val *string
		switch req.SortBy {
		case "last_trade":
			if last.LastTradeAt != nil {
				v := last.LastTradeAt.UTC().Format(time.RFC3339Nano)
				val = &v
			}
		default:
			val = metricString(req.SortBy, last)
		}
		cur, err := EncodeCursor(s.secret, fp, val, last.WalletAddress)
		if err != nil {
			return nil, err
		}
		res.NextCursor = cur
	}
	return res, nil
}

// countTotal runs the COUNT(*) with filters identical to the data query.
func (s *Service) countTotal(ctx context.Context, req *SearchRequest, venueID uuid.UUID, groupID *uuid.UUID) (int64, error) {
	query, args := buildSearchCountQuery(req, venueID, groupID)
	return s.repo.countSearch(ctx, query, args)
}

func metricString(sortBy string, m *PeriodMetrics) *string {
	var f *float64
	var i *int64
	switch sortBy {
	case "pnl":
		f = m.PnL
	case "roi":
		f = m.ROI
	case "win_rate":
		f = m.WinRate
	case "volume":
		f = m.Volume
	case "trade_count":
		if m.TradeCount != nil {
			v := *m.TradeCount
			i = &v
		}
	}
	if f != nil {
		v := fmt.Sprintf("%v", *f)
		return &v
	}
	if i != nil {
		v := fmt.Sprintf("%d", *i)
		return &v
	}
	return nil
}

// Detail returns the LIVE trader overview (LIVE-CONTRACT v1.2 §1.7, was DB).
// Registry header is a light DB read (404 unknown wallet/venue). Metrics are
// computed live from the same fills universe as activity/performance
// (1D/7D/30D from fills via ReconstructTrades + computeLiveMetrics; ALL from
// portfolio allTime + leaderboard refs passthrough, live-read — same as
// Performance via fetchLivePerformance reuse). No reads of
// trader_period_metrics / trader_positions / trader_trades (conformance §7);
// scanner search still reads period_metrics (unchanged). data_status ∈
// {ready,error} ONLY (never syncing/stale); as_of = fetch time (null on
// error without cache). TTL 12s + single-flight per (wallet,venue,period);
// HL error ⇒ stale with error else metrics null (never 500).
func (s *Service) Detail(ctx context.Context, venueCode, rawAddr, period string) (*Detail, error) {
	addr, err := NormalizeAddress(rawAddr)
	if err != nil {
		return nil, err
	}
	period = strings.ToUpper(strings.TrimSpace(period))
	if period == "" {
		period = Period30D
	}
	switch period {
	case Period1D, Period7D, Period30D, PeriodALL:
	default:
		return nil, domain.NewError(domain.ErrCodeInvalidFilter, "invalid period")
	}
	venue := strings.ToLower(strings.TrimSpace(venueCode))
	if venue == "" {
		venue = DefaultVenue
	}
	venueID, err := s.repo.VenueIDByCode(ctx, venue)
	if err != nil {
		return nil, domain.NewError(domain.ErrCodeInvalidFilter, "unknown venue")
	}
	reg, err := s.repo.GetRegistry(ctx, venueID, addr)
	if err != nil {
		return nil, err
	}
	// LIVE: no EnsureSyncState, no GetPeriodMetrics (WS-E teardown).
	key := "detail-live|" + venue + "|" + addr + "|" + period
	now := time.Now().UTC()
	val, _, _, fetchErr := s.cache.GetOrFetch(key, func() (any, error) {
		return fetchLivePerformance(ctx, s, venueID, addr, venue, period, now)
	})
	if fetchErr != nil {
		if stale, _, found := s.cache.get(key); found {
			if snap, ok := stale.(*LivePerformanceSnapshot); ok && snap != nil {
				asOf := snap.AsOf.UTC()
				m := liveSnapshotToPeriodMetrics(snap, venueID, venue, addr, period, reg.DisplayName, DataError)
				return &Detail{Registry: reg, Metrics: m, Period: period, DataStatus: DataError, AsOf: &asOf}, nil
			}
		}
		return &Detail{Registry: reg, Metrics: nil, Period: period, DataStatus: DataError, AsOf: nil}, nil
	}
	snap, ok := val.(*LivePerformanceSnapshot)
	if !ok || snap == nil {
		return &Detail{Registry: reg, Metrics: nil, Period: period, DataStatus: DataError, AsOf: nil}, nil
	}
	asOf := snap.AsOf.UTC()
	m := liveSnapshotToPeriodMetrics(snap, venueID, venue, addr, period, reg.DisplayName, DataReady)
	// ALL equity-only (LB missing, portfolio ok): metrics null, still ready.
	return &Detail{Registry: reg, Metrics: m, Period: period, DataStatus: DataReady, AsOf: &asOf}, nil
}

// liveSnapshotToPeriodMetrics converts a live performance snapshot into the
// Detail overview metrics shape (PeriodMetrics). Overlapping fields map 1:1
// (pnl/realized_pnl, roi, win_rate, trade_count, volume, profit_factor,
// long/short wins+counts, max_drawdown); non-live columns stay nil. Nil
// snapshot metrics (ALL equity-only) maps to nil metrics (still ready at the
// Detail level). data_status mirrors the caller status (ready/error only);
// as_of = snapshot fetch time; calculation_version stamps the live formula.
func liveSnapshotToPeriodMetrics(snap *LivePerformanceSnapshot, venueID uuid.UUID, venue, addr, period string, displayName *string, status DataStatus) *PeriodMetrics {
	if snap == nil || snap.Metrics == nil {
		return nil
	}
	pm := snap.Metrics
	asOf := snap.AsOf.UTC()
	return &PeriodMetrics{
		VenueID: venueID, Venue: venue, WalletAddress: addr, DisplayName: displayName,
		Period: period, AsOf: asOf,
		PnL: pm.PnL, RealizedPnL: pm.PnL, ROI: pm.ROI, WinRate: pm.WinRate,
		TradeCount: pm.TradeCount, Volume: pm.Volume, ProfitFactor: pm.ProfitFactor,
		LongCount: pm.LongCount, LongWins: pm.LongWins,
		ShortCount: pm.ShortCount, ShortWins: pm.ShortWins,
		MaxDrawdownPct:     pm.MaxDrawdownPct,
		DataStatus:         status,
		IsPartial:          snap.Partial,
		CalculationVersion: CurrentCalculationVersion,
	}
}

// Detail is the GET /api/v1/traders/{wallet} payload (LIVE-CONTRACT v1.2
// §1.7). Registry + Period shape kept; DataStatus ∈ {ready,error} ONLY with
// AsOf = live fetch time (null on error without cache). Metrics is the live
// overview (nil only on error without cache or ALL equity-only).
type Detail struct {
	Registry   *RegistryEntry `json:"registry"`
	Metrics    *PeriodMetrics `json:"metrics"`
	Period     string         `json:"period"`
	DataStatus DataStatus     `json:"data_status"`
	AsOf       *time.Time     `json:"as_of"`
}

// PositionDTO is one open position row (snake_case per DETAIL-PLAN §4.1).
type PositionDTO struct {
	Coin             string     `json:"coin"`
	Side             string     `json:"side"`
	Size             float64    `json:"size"`
	EntryPrice       *float64   `json:"entry_price"`
	MarkPrice        *float64   `json:"mark_price"`
	PositionValue    *float64   `json:"position_value"`
	UnrealizedPnl    *float64   `json:"unrealized_pnl"`
	ReturnOnEquity   *float64   `json:"return_on_equity"`
	LiquidationPrice *float64   `json:"liquidation_price"`
	Leverage         *float64   `json:"leverage"`
	MaxLeverage      *float64   `json:"max_leverage"`
	MarginUsed       *float64   `json:"margin_used"`
	AsOf             *time.Time `json:"as_of"`
}

// PositionSummaryDTO is the account-level margin summary (null when never synced).
type PositionSummaryDTO struct {
	AccountValue    *float64   `json:"account_value"`
	TotalNtlPos     *float64   `json:"total_ntl_pos"`
	TotalMarginUsed *float64   `json:"total_margin_used"`
	AsOf            *time.Time `json:"as_of"`
}

// PositionSnapshotDTO is the GET /traders/{wallet}/positions payload (§4.1).
type PositionSnapshotDTO struct {
	Summary    *PositionSummaryDTO `json:"summary"`
	Positions  []PositionDTO       `json:"positions"`
	DataStatus DataStatus          `json:"data_status"`
	AsOf       *time.Time          `json:"as_of"`
}

// ActivityTradeDTO is one live closed trade with server-computed net_pnl
// (LIVE-CONTRACT v1.2 §1.2). DurationSec = closed-open seconds;
// entry_price/exit_price are avg open/close legs (never NULL for live rows;
// pre-000030 DB rows kept NULL for backward compat). funding is signed
// (negative = paid), informational only: net_pnl = pnl − fees (UNCHANGED),
// win/loss/counts still on net_pnl.
type ActivityTradeDTO struct {
	Market      string    `json:"market"`
	Side        string    `json:"side"`
	OpenedAt    time.Time `json:"opened_at"`
	ClosedAt    time.Time `json:"closed_at"`
	DurationSec float64   `json:"duration_sec"`
	Volume      float64   `json:"volume"`
	EntryPrice  *float64  `json:"entry_price"`
	ExitPrice   *float64  `json:"exit_price"`
	PnL         float64   `json:"pnl"`
	Fees        float64   `json:"fees"`
	Funding     float64   `json:"funding"`
	NetPnl      float64   `json:"net_pnl"`
	Fills       int       `json:"fills"`
}

// ActivityCounts are totals across the whole retained window (independent of
// pagination, result and side filters) so the chips are stable (§1.2).
// Win = net>0, loss = net<0 (breakeven in total only); long/short by side.
type ActivityCounts struct {
	Win   int `json:"win"`
	Loss  int `json:"loss"`
	Long  int `json:"long"`
	Short int `json:"short"`
	Total int `json:"total"`
}

// ActivityPage is the GET /traders/{wallet}/activity payload
// (LIVE-CONTRACT v1.2 §1.2: LIVE, was DB). Rows from the trailing 30d fills
// window (in-memory ReconstructTrades + funding attribution). counts over the
// fetched 30d universe (stable across pagination/filters). data_status ∈
// {ready,error} ONLY (never syncing/stale); as_of = fetch time; partial=true
// when the venue truncated the window (10k-fill cap, timeouts). Rows never
// null.
type ActivityPage struct {
	Rows       []ActivityTradeDTO `json:"rows"`
	NextCursor string             `json:"next_cursor,omitempty"`
	HasMore    bool               `json:"has_more"`
	Counts     ActivityCounts     `json:"counts"`
	DataStatus DataStatus         `json:"data_status"`
	AsOf       *time.Time         `json:"as_of"`
	Partial    bool               `json:"partial"`
}

// ActivityQuery carries the §1.2 query params (defaults applied by the handler
// or Normalize: sort=closed_at, dir=desc, result=all, side=all).
type ActivityQuery struct {
	Limit  int
	Cursor string
	Sort   string
	Dir    string
	Result string
	Side   string
}

// activityCursor is the HMAC-sealed keyset triple
// (closed_at DESC, market ASC, opened_at ASC).
type activityCursor struct {
	FP string `json:"fp"`
	C  string `json:"c"` // closed_at RFC3339Nano
	M  string `json:"m"` // market tiebreak
	O  string `json:"o"` // opened_at RFC3339Nano
}

func activityFingerprint(venueID uuid.UUID, addr string) string {
	sum := sha256.Sum256([]byte(venueID.String() + "|" + addr))
	return fmt.Sprintf("%x", sum[:])
}

// EncodeActivityCursor seals the page triple (closed_at, market, opened_at).
func EncodeActivityCursor(secret []byte, fp string, closedAt time.Time, market string, openedAt time.Time) (string, error) {
	raw, err := json.Marshal(activityCursor{
		FP: fp, C: closedAt.UTC().Format(time.RFC3339Nano),
		M: market, O: openedAt.UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(raw)
	sig := mac.Sum(nil)
	return base64.RawURLEncoding.EncodeToString(raw) + "." +
		base64.RawURLEncoding.EncodeToString(sig), nil
}

// DecodeActivityCursor verifies and opens an activity cursor bound to fp.
func DecodeActivityCursor(secret []byte, fp, raw string) (closedAt time.Time, market string, openedAt time.Time, err error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 2 {
		return time.Time{}, "", time.Time{}, invalidFilter("malformed cursor")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return time.Time{}, "", time.Time{}, invalidFilter("malformed cursor")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}, "", time.Time{}, invalidFilter("malformed cursor")
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(payload)
	if !hmac.Equal(mac.Sum(nil), sig) {
		return time.Time{}, "", time.Time{}, invalidFilter("invalid cursor signature")
	}
	var c activityCursor
	if err := json.Unmarshal(payload, &c); err != nil {
		return time.Time{}, "", time.Time{}, invalidFilter("malformed cursor")
	}
	if c.FP != fp {
		return time.Time{}, "", time.Time{}, invalidFilter("cursor does not match this query")
	}
	if c.M == "" {
		return time.Time{}, "", time.Time{}, invalidFilter("malformed cursor")
	}
	ct, err := time.Parse(time.RFC3339Nano, c.C)
	if err != nil {
		return time.Time{}, "", time.Time{}, invalidFilter("malformed cursor")
	}
	ot, err := time.Parse(time.RFC3339Nano, c.O)
	if err != nil {
		return time.Time{}, "", time.Time{}, invalidFilter("malformed cursor")
	}
	return ct.UTC(), c.M, ot.UTC(), nil
}

// Positions returns the LIVE open-position snapshot (LIVE-CONTRACT v1.2 §1.1).
// Source: live clearinghouseState per request behind the interactive client
// (own pacer floor ~200ms, concurrency ~4, ctx timeout) + TTL 12s cache +
// single-flight. No DB reads (zero queries to trader_positions;
// conformance §7). Unknown wallet → 404.
// sort ∈ {coin,size,entry_price,mark_price,position_value,unrealized_pnl,
// return_on_equity,leverage} default coin; dir ∈ {asc,desc} default asc.
// Null numerics sort last regardless of dir; tiebreak coin ASC.
// data_status ∈ {ready,error} ONLY (never syncing/stale); as_of = fetch time
// (never null on success). HL error ⇒ last cached with error, else empty
// rows (never null).
func (s *Service) Positions(ctx context.Context, venueCode, rawAddr, sort, dir string) (*PositionSnapshotDTO, error) {
	addr, err := NormalizeAddress(rawAddr)
	if err != nil {
		return nil, err
	}
	sort = strings.ToLower(strings.TrimSpace(sort))
	if sort == "" {
		sort = "coin"
	}
	dir = strings.ToLower(strings.TrimSpace(dir))
	if dir == "" {
		dir = "asc"
	}
	if !validPositionSort(sort) {
		return nil, domain.NewError(domain.ErrCodeInvalidFilter, "invalid sort")
	}
	if dir != "asc" && dir != "desc" {
		return nil, domain.NewError(domain.ErrCodeInvalidFilter, "invalid dir: want asc|desc")
	}
	venue := strings.ToLower(strings.TrimSpace(venueCode))
	if venue == "" {
		venue = DefaultVenue
	}
	venueID, err := s.repo.VenueIDByCode(ctx, venue)
	if err != nil {
		return nil, domain.NewError(domain.ErrCodeInvalidFilter, "unknown venue")
	}
	if _, err := s.repo.GetRegistry(ctx, venueID, addr); err != nil {
		return nil, err
	}
	key := "positions-live|" + venue + "|" + addr
	cached, freshHit, _, fetchErr := s.cache.GetOrFetch(key, func() (any, error) {
		if s.onDemand == nil {
			return nil, errUpstreamUnavailable
		}
		state, err := s.onDemand.FetchClearinghouseState(ctx, addr)
		if err != nil {
			return nil, err
		}
		now := time.Now().UTC()
		return MapClearinghouseToSnapshot(state, now), nil
	})
	_ = freshHit
	if fetchErr != nil {
		if cached != nil {
			if snap, ok := cached.(*PositionSnapshot); ok {
				return snapshotToDTO(snap, sort, dir, DataError), nil
			}
		}
		if stale, _, found := s.cache.get(key); found {
			if snap, ok := stale.(*PositionSnapshot); ok {
				return snapshotToDTO(snap, sort, dir, DataError), nil
			}
		}
		return &PositionSnapshotDTO{
			Summary: nil, Positions: []PositionDTO{}, DataStatus: DataError, AsOf: nil,
		}, nil
	}
	snap, ok := cached.(*PositionSnapshot)
	if !ok || snap == nil {
		return &PositionSnapshotDTO{
			Summary: nil, Positions: []PositionDTO{}, DataStatus: DataError, AsOf: nil,
		}, nil
	}
	return snapshotToDTO(snap, sort, dir, DataReady), nil
}

func snapshotToDTO(snap *PositionSnapshot, sort, dir string, status DataStatus) *PositionSnapshotDTO {
	asOf := snap.AsOf.UTC()
	positions := append([]Position(nil), snap.Positions...)
	sortPositions(positions, sort, dir)
	rows := make([]PositionDTO, 0, len(positions))
	for _, p := range positions {
		rows = append(rows, PositionDTO{
			Coin: p.Coin, Side: p.Side, Size: p.Size,
			EntryPrice: p.EntryPrice, MarkPrice: p.MarkPrice,
			PositionValue: p.PositionValue, UnrealizedPnl: p.UnrealizedPnl,
			ReturnOnEquity: p.ReturnOnEquity, LiquidationPrice: p.LiquidationPrice,
			Leverage: p.Leverage, MaxLeverage: p.MaxLeverage,
			MarginUsed: p.MarginUsed, AsOf: &asOf,
		})
	}
	summary := &PositionSummaryDTO{
		AccountValue: snap.AccountValue, TotalNtlPos: snap.TotalNtlPos,
		TotalMarginUsed: snap.TotalMarginUsed, AsOf: &asOf,
	}
	return &PositionSnapshotDTO{
		Summary: summary, Positions: rows, DataStatus: status, AsOf: &asOf,
	}
}

func validPositionSort(sort string) bool {
	switch sort {
	case "coin", "size", "entry_price", "mark_price", "position_value",
		"unrealized_pnl", "return_on_equity", "leverage":
		return true
	}
	return false
}

// WS-E: deriveActivityStatus (syncing/stale machinery) removed. Detail
// data_status ∈ {ready,error} ONLY (LIVE-CONTRACT v1.2 §1.1/§1.2/§1.7).

// Activity returns LIVE closed trades (LIVE-CONTRACT v1.2 §1.2, was DB).
// Source: live userFillsByTime over the trailing 30d → in-memory
// ReconstructTrades (reuse) → rows with entry_price/exit_price (avg),
// duration_sec, volume, pnl, fees, funding (signed, negative = paid),
// net_pnl = pnl − fees (UNCHANGED), fills. Funding attribution: sum
// userFunding payments with openTime ≤ time ≤ closeTime per coin,
// informational only; win/loss/counts still on net_pnl.
// counts over the fetched 30d window (stable across pagination/filters).
// Cursor: same HMAC keyset scheme, computed in-memory (reuse helpers).
// data_status ∈ {ready,error} ONLY; plus as_of + partial (true when the venue
// truncated the window, e.g. 10k-fill cap, or on timeout fallback). Rows never
// null. No DB reads (zero queries to trader_trades; conformance §7).
// limit 1..100 default 20; sort ∈ {closed_at,opened_at,market,volume,pnl,
// net_pnl,duration,entry_price,exit_price} default closed_at; dir ∈ {asc,desc}
// default desc; result ∈ {all,win,loss} default all; side ∈ {all,long,short}
// default all. Unknown wallet → 404.
func (s *Service) Activity(ctx context.Context, venueCode, rawAddr string, q ActivityQuery) (*ActivityPage, error) {
	addr, err := NormalizeAddress(rawAddr)
	if err != nil {
		return nil, err
	}
	limit := q.Limit
	if limit == 0 {
		limit = 20
	}
	if limit < 1 || limit > 100 {
		return nil, domain.NewError(domain.ErrCodeInvalidFilter, "limit must be 1..100")
	}
	sort := strings.ToLower(strings.TrimSpace(q.Sort))
	if sort == "" {
		sort = "closed_at"
	}
	dir := strings.ToLower(strings.TrimSpace(q.Dir))
	if dir == "" {
		dir = "desc"
	}
	result := strings.ToLower(strings.TrimSpace(q.Result))
	if result == "" {
		result = "all"
	}
	side := strings.ToLower(strings.TrimSpace(q.Side))
	if side == "" {
		side = "all"
	}
	if !validActivitySort(sort) {
		return nil, domain.NewError(domain.ErrCodeInvalidFilter, "invalid sort")
	}
	if dir != "asc" && dir != "desc" {
		return nil, domain.NewError(domain.ErrCodeInvalidFilter, "invalid dir: want asc|desc")
	}
	if result != "all" && result != "win" && result != "loss" {
		return nil, domain.NewError(domain.ErrCodeInvalidFilter, "invalid result: want all|win|loss")
	}
	if side != "all" && side != "long" && side != "short" {
		return nil, domain.NewError(domain.ErrCodeInvalidFilter, "invalid side: want all|long|short")
	}
	venue := strings.ToLower(strings.TrimSpace(venueCode))
	if venue == "" {
		venue = DefaultVenue
	}
	venueID, err := s.repo.VenueIDByCode(ctx, venue)
	if err != nil {
		return nil, domain.NewError(domain.ErrCodeInvalidFilter, "unknown venue")
	}
	if _, err := s.repo.GetRegistry(ctx, venueID, addr); err != nil {
		return nil, err
	}

	fp := activityQueryFingerprint(venueID, addr, sort, dir, result, side)
	var after *activityAfter
	if q.Cursor != "" {
		a, err := decodeActivityCursorV2(s.secret, fp, q.Cursor)
		if err != nil {
			return nil, err
		}
		after = a
	}

	key := "activity-live|" + venue + "|" + addr
	now := time.Now().UTC()
	val, _, _, fetchErr := s.cache.GetOrFetch(key, func() (any, error) {
		return fetchLiveActivity(ctx, s.onDemand, addr, now)
	})
	var snap *LiveActivitySnapshot
	if fetchErr != nil {
		if stale, _, found := s.cache.get(key); found {
			if st, ok := stale.(*LiveActivitySnapshot); ok {
				snap = st
			}
		}
		if snap == nil {
			// No data: empty universe with error + partial (truncated/timeout
			// fallback), never null rows.
			return &ActivityPage{
				Rows: []ActivityTradeDTO{}, HasMore: false,
				Counts: ActivityCounts{}, DataStatus: DataError,
				AsOf: nil, Partial: true,
			}, nil
		}
		return activityPageFromSnapshot(s, fp, snap, after, sort, dir, result, side, limit, DataError), nil
	}
	st, ok := val.(*LiveActivitySnapshot)
	if !ok || st == nil {
		return &ActivityPage{
			Rows: []ActivityTradeDTO{}, HasMore: false,
			Counts: ActivityCounts{}, DataStatus: DataError,
			AsOf: nil, Partial: true,
		}, nil
	}
	return activityPageFromSnapshot(s, fp, st, after, sort, dir, result, side, limit, DataReady), nil
}

func activityPageFromSnapshot(s *Service, fp string, snap *LiveActivitySnapshot, after *activityAfter, sort, dir, result, side string, limit int, status DataStatus) *ActivityPage {
	counts := activityCounts(snap.Rows)
	filtered := filterActivityRows(snap.Rows, result, side)
	// filterActivityRows returns a fresh slice; sort in place is safe.
	sortActivityRows(filtered, sort, dir)
	paged := applyActivityCursor(filtered, sort, dir, after)
	hasMore := len(paged) > limit
	if hasMore {
		paged = paged[:limit]
	}
	rows := make([]ActivityTradeDTO, 0, len(paged))
	for _, r := range paged {
		rows = append(rows, ActivityTradeDTO{
			Market: r.Market, Side: r.Side,
			OpenedAt: r.OpenedAt.UTC(), ClosedAt: r.ClosedAt.UTC(),
			DurationSec: r.ClosedAt.Sub(r.OpenedAt).Seconds(),
			Volume:      r.Volume, EntryPrice: r.EntryPrice, ExitPrice: r.ExitPrice,
			PnL: r.PnL, Fees: r.Fees, Funding: r.Funding,
			NetPnl: r.PnL - r.Fees, Fills: r.Fills,
		})
	}
	asOf := snap.AsOf.UTC()
	page := &ActivityPage{Rows: rows, HasMore: hasMore, Counts: counts,
		DataStatus: status, AsOf: &asOf, Partial: snap.Partial}
	if hasMore {
		last := paged[len(paged)-1]
		if cur, err := encodeActivityCursorV2(s.secret, fp, sort, last); err == nil {
			page.NextCursor = cur
		}
	}
	return page
}
