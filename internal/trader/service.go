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

// SearchResult is one page: rows + opaque cursor for the next page.
type SearchResult struct {
	Rows       []*PeriodMetrics `json:"rows"`
	NextCursor string           `json:"next_cursor,omitempty"`
	HasMore    bool             `json:"has_more"`
}

// Search validates, resolves venue/group, applies the keyset cursor and reads
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
	res := &SearchResult{Rows: []*PeriodMetrics{}}
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
