package wallet

import (
	"context"
	"errors"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/qwerty7415963/go_be_arbitrage/internal/api"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

// RepositoryInterface is the data dependency of Service; *Repository
// implements it in production, tests substitute a mock.
type RepositoryInterface interface {
	ScanWallets(ctx context.Context, f *Filters, sort *SortSpec, groupID *uuid.UUID, limit, offset int) ([]*Wallet, int64, error)
	GetDetail(ctx context.Context, id, userID uuid.UUID, f *Filters) (*WalletDetail, error)
	GetGroupOwner(ctx context.Context, groupID uuid.UUID) (uuid.UUID, bool, error)
}

type Service struct {
	repo RepositoryInterface
	cfg  FilterConfig
}

func NewService(repo RepositoryInterface, cfg FilterConfig) *Service {
	return &Service{repo: repo, cfg: cfg}
}

func metaFor(page, limit, offset int, total int64) *api.Meta {
	totalPages := int((total + int64(limit) - 1) / int64(limit))
	return &api.Meta{
		Page:       page,
		Limit:      limit,
		Offset:     offset,
		TotalPages: totalPages,
		HasMore:    offset+limit < int(total),
	}
}

// Scan runs the global wallet scanner (BE-02): parse → validate →
// offset-paginated deterministic scan.
func (s *Service) Scan(ctx context.Context, query url.Values) ([]*Wallet, *api.Meta, error) {
	f, sort, page, limit, err := s.parse(query)
	if err != nil {
		return nil, nil, err
	}
	wallets, total, err := s.repo.ScanWallets(ctx, f, sort, nil, limit, (page-1)*limit)
	if err != nil {
		return nil, nil, domain.WrapError(domain.ErrCodeInternal, "scan failed", err)
	}
	return wallets, metaFor(page, limit, (page-1)*limit, total), nil
}

// ScanGroupWallets is the group-scoped scanner (BE-09): same grammar,
// ownership enforced first (GROUP-001 unknown / GROUP-003 foreign), rows
// restricted to the group's members.
func (s *Service) ScanGroupWallets(ctx context.Context, userID, groupID uuid.UUID, query url.Values) ([]*Wallet, *api.Meta, error) {
	if err := s.requireGroup(ctx, userID, groupID); err != nil {
		return nil, nil, err
	}
	f, sort, page, limit, err := s.parse(query)
	if err != nil {
		return nil, nil, err
	}
	wallets, total, err := s.repo.ScanWallets(ctx, f, sort, &groupID, limit, (page-1)*limit)
	if err != nil {
		return nil, nil, domain.WrapError(domain.ErrCodeInternal, "scan failed", err)
	}
	return wallets, metaFor(page, limit, (page-1)*limit, total), nil
}

// Detail returns one wallet with its metrics and the caller's own group
// memberships only (BE-06); foreign memberships are never loaded.
func (s *Service) Detail(ctx context.Context, userID, id uuid.UUID, query url.Values) (*WalletDetail, error) {
	f, _, _, _, err := s.parse(query)
	if err != nil {
		return nil, err
	}
	detail, err := s.repo.GetDetail(ctx, id, userID, f)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.NewError(domain.ErrCodeWalletNotFound, "wallet not found")
	}
	if err != nil {
		return nil, domain.WrapError(domain.ErrCodeInternal, "wallet lookup failed", err)
	}
	return detail, nil
}

func (s *Service) parse(query url.Values) (*Filters, *SortSpec, int, int, error) {
	f, err := ParseFilters(query, s.cfg)
	if err != nil {
		return nil, nil, 0, 0, err
	}
	sort, err := ParseSort(query)
	if err != nil {
		return nil, nil, 0, 0, err
	}
	page, limit := ParsePageLimit(query)
	return f, sort, page, limit, nil
}

func (s *Service) requireGroup(ctx context.Context, userID, groupID uuid.UUID) error {
	owner, found, err := s.repo.GetGroupOwner(ctx, groupID)
	if err != nil {
		return domain.WrapError(domain.ErrCodeInternal, "group lookup failed", err)
	}
	if !found {
		return domain.NewError(domain.ErrCodeGroupNotFound, "group not found")
	}
	if owner != userID {
		return domain.NewError(domain.ErrCodeGroupForbidden, "group not accessible")
	}
	return nil
}

// Now is a seam for deterministic tests of relative windows.
var Now = time.Now
