package trader

import (
	"context"
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
}

func NewService(repo *Repository, groups GroupOwner, cursorSecret []byte) *Service {
	return &Service{repo: repo, groups: groups, secret: cursorSecret}
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
