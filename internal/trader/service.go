package trader

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

// GroupOwner resolves a search group_id's owner (*tradergroup.Repository
// implements it; declared here so trader never imports the groups package).
type GroupOwner interface {
	OwnerOf(ctx context.Context, groupID uuid.UUID) (uuid.UUID, error)
}

// Service serves the v1.1 scanner reads. Search/detail are public (parity with
// the legacy scanner); the group filter and trader-groups stay auth-gated.
type Service struct {
	repo   *Repository
	groups GroupOwner
	secret []byte
	// positionsStaleAfter bounds positions freshness (DETAIL-PLAN A6):
	// last_positions_sync_at older than this reports stale; NULL reports
	// syncing (M5). Default 5m (near-realtime 30s refresh for watched).
	positionsStaleAfter time.Duration
	// onDemand serves the wallet-tabs on-demand reads (contract WALLET-TABS
	// v1 §1.3-§1.7, WS2-WS6). Nil disables HL reads (unit contexts without a
	// venue client); handlers then surface data_status=error via the mock.
	onDemand OnDemandClient
	cache    *OnDemandCache
}

func NewService(repo *Repository, groups GroupOwner, cursorSecret []byte) *Service {
	return &Service{repo: repo, groups: groups, secret: cursorSecret,
		positionsStaleAfter: 5 * time.Minute, cache: NewOnDemandCache(15 * time.Second)}
}

// WithPositionsStaleAfter overrides the positions stale threshold (tests).
func (s *Service) WithPositionsStaleAfter(d time.Duration) *Service {
	s.positionsStaleAfter = d
	return s
}

// WithOnDemand attaches the venue on-demand client + cache (WS2-WS6).
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

// Detail returns the trader header + one period's metrics (nil when never
// synced). It never calls upstream inline; it ensures a sync-state row so the
// scheduler prioritizes pending wallets (spec BE-038).
func (s *Service) Detail(ctx context.Context, venueCode, rawAddr, period string) (*Detail, error) {
	addr, err := NormalizeAddress(rawAddr)
	if err != nil {
		return nil, err
	}
	if period == "" {
		period = Period30D
	}
	switch period {
	case Period1D, Period7D, Period30D, PeriodALL:
	default:
		return nil, domain.NewError(domain.ErrCodeInvalidFilter, "invalid period")
	}
	venueID, err := s.repo.VenueIDByCode(ctx, strings.ToLower(strings.TrimSpace(venueCode)))
	if err != nil {
		return nil, domain.NewError(domain.ErrCodeInvalidFilter, "unknown venue")
	}
	reg, err := s.repo.GetRegistry(ctx, venueID, addr)
	if err != nil {
		return nil, err
	}
	// On-demand priority without inline sync (BE-038).
	if _, err := s.repo.EnsureSyncState(ctx, venueID, addr); err != nil {
		return nil, err
	}
	m, err := s.repo.GetPeriodMetrics(ctx, venueID, addr, period)
	if err != nil {
		return nil, err
	}
	return &Detail{Registry: reg, Metrics: m, Period: period}, nil
}

// Detail is the GET /api/v1/traders/{wallet} payload.
type Detail struct {
	Registry *RegistryEntry `json:"registry"`
	Metrics  *PeriodMetrics `json:"metrics"`
	Period   string         `json:"period"`
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

// ActivityTradeDTO is one durable closed trade with server-computed net_pnl.
// DurationSec = closed-open seconds; Entry/Exit NULL for pre-000030 rows.
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

// ActivityPage is the GET /traders/{wallet}/activity payload (§1.2).
type ActivityPage struct {
	Rows       []ActivityTradeDTO `json:"rows"`
	NextCursor string             `json:"next_cursor,omitempty"`
	HasMore    bool               `json:"has_more"`
	Counts     ActivityCounts     `json:"counts"`
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

// Positions returns the latest open-position snapshot (DETAIL-PLAN A6 +
// WALLET-TABS v1 §1.1: server-side sort). It never calls upstream inline;
// freshness comes from trader_sync_state.last_positions_sync_at: NULL (never
// synced) reports syncing (M5), older than the stale threshold reports stale,
// else ready. Unknown wallet → 404 (Detail error path).
// sort ∈ {coin,size,entry_price,mark_price,position_value,unrealized_pnl,
// return_on_equity,leverage} default coin; dir ∈ {asc,desc} default asc.
// Null numerics sort last regardless of dir; tiebreak coin ASC.
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
	venueID, err := s.repo.VenueIDByCode(ctx, strings.ToLower(strings.TrimSpace(venueCode)))
	if err != nil {
		return nil, domain.NewError(domain.ErrCodeInvalidFilter, "unknown venue")
	}
	if _, err := s.repo.GetRegistry(ctx, venueID, addr); err != nil {
		return nil, err
	}
	state, err := s.repo.EnsureSyncState(ctx, venueID, addr)
	if err != nil {
		return nil, err
	}
	summary, err := s.repo.GetPositionSummary(ctx, venueID, addr)
	if err != nil {
		return nil, err
	}
	positions, err := s.repo.GetPositions(ctx, venueID, addr)
	if err != nil {
		return nil, err
	}

	status := DataSyncing // M5: never synced reports syncing, never stale.
	var asOf *time.Time
	if state != nil && state.LastPositionsSyncAt != nil {
		status = DataReady
		if s.positionsStaleAfter > 0 && time.Since(*state.LastPositionsSyncAt) > s.positionsStaleAfter {
			status = DataStale
		}
	}
	var summaryDTO *PositionSummaryDTO
	if summary != nil {
		t := summary.AsOf.UTC()
		summaryDTO = &PositionSummaryDTO{
			AccountValue: summary.AccountValue, TotalNtlPos: summary.TotalNtlPos,
			TotalMarginUsed: summary.TotalMarginUsed, AsOf: &t,
		}
		asOf = &t
	}

	sortPositions(positions, sort, dir)
	rows := make([]PositionDTO, 0, len(positions))
	for _, p := range positions {
		rows = append(rows, PositionDTO{
			Coin: p.Coin, Side: p.Side, Size: p.Size,
			EntryPrice: p.EntryPrice, MarkPrice: p.MarkPrice,
			PositionValue: p.PositionValue, UnrealizedPnl: p.UnrealizedPnl,
			ReturnOnEquity: p.ReturnOnEquity, LiquidationPrice: p.LiquidationPrice,
			Leverage: p.Leverage, MaxLeverage: p.MaxLeverage,
			MarginUsed: p.MarginUsed, AsOf: asOf,
		})
	}
	return &PositionSnapshotDTO{
		Summary: summaryDTO, Positions: rows, DataStatus: status, AsOf: asOf,
	}, nil
}

func validPositionSort(sort string) bool {
	switch sort {
	case "coin", "size", "entry_price", "mark_price", "position_value",
		"unrealized_pnl", "return_on_equity", "leverage":
		return true
	}
	return false
}

// Activity returns durable closed trades with server-side sort/filter and
// keyset cursor + has_more + stable counts (WALLET-TABS v1 §1.2).
// limit 1..100 default 20; sort ∈ {closed_at,opened_at,market,volume,pnl,
// net_pnl,duration,entry_price,exit_price} default closed_at; dir ∈ {asc,desc}
// default desc; result ∈ {all,win,loss} default all; side ∈ {all,long,short}
// default all. net_pnl = pnl - fees server-side. counts across the whole
// retained window independent of pagination/result/side. Cursor HMAC-sealed
// over (sortKey, closed_at, market, opened_at) honouring dir. Unknown wallet
// → 404. Backwards compat: legacy (closed_at DESC, market ASC, opened_at ASC)
// triple cursors are NOT accepted here; callers refetch page 1 on 400.
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
	venueID, err := s.repo.VenueIDByCode(ctx, strings.ToLower(strings.TrimSpace(venueCode)))
	if err != nil {
		return nil, domain.NewError(domain.ErrCodeInvalidFilter, "unknown venue")
	}
	if _, err := s.repo.GetRegistry(ctx, venueID, addr); err != nil {
		return nil, err
	}
	if _, err := s.repo.EnsureSyncState(ctx, venueID, addr); err != nil {
		return nil, err
	}

	all, err := s.repo.ListAllTrades(ctx, venueID, addr)
	if err != nil {
		return nil, err
	}
	counts := activityCounts(all)
	filtered := filterActivityRows(all, result, side)
	sortActivityRows(filtered, sort, dir)

	fp := activityQueryFingerprint(venueID, addr, sort, dir, result, side)
	var after *activityAfter
	if q.Cursor != "" {
		a, err := decodeActivityCursorV2(s.secret, fp, q.Cursor)
		if err != nil {
			return nil, err
		}
		after = a
	}
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
			PnL: r.PnL, Fees: r.Fees,
			NetPnl: r.PnL - r.Fees, Fills: r.Fills,
		})
	}
	page := &ActivityPage{Rows: rows, HasMore: hasMore, Counts: counts}
	if hasMore {
		last := paged[len(paged)-1]
		cur, err := encodeActivityCursorV2(s.secret, fp, sort, last)
		if err != nil {
			return nil, err
		}
		page.NextCursor = cur
	}
	return page, nil
}
