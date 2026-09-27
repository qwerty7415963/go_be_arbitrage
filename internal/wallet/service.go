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
	ScanWallets(ctx context.Context, f *Filters, sort *SortSpec, groupID *uuid.UUID, userID uuid.UUID, limit, offset int) ([]*Wallet, int64, error)
	GetDetail(ctx context.Context, id, userID uuid.UUID, f *Filters) (*WalletDetail, error)
	GetGroupOwner(ctx context.Context, groupID uuid.UUID) (uuid.UUID, bool, error)
	WalletExists(ctx context.Context, id uuid.UUID) (bool, error)
	UpsertTag(ctx context.Context, userID, walletID uuid.UUID, tag string) error
	ClearTag(ctx context.Context, userID, walletID uuid.UUID) error
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
// offset-paginated deterministic scan. Rows carry the caller's own tag.
func (s *Service) Scan(ctx context.Context, userID uuid.UUID, query url.Values) ([]*Wallet, *api.Meta, error) {
	f, sort, page, limit, err := s.parse(query)
	if err != nil {
		return nil, nil, err
	}
	wallets, total, err := s.repo.ScanWallets(ctx, f, sort, nil, userID, limit, (page-1)*limit)
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
	wallets, total, err := s.repo.ScanWallets(ctx, f, sort, &groupID, userID, limit, (page-1)*limit)
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

// UpdateTag sets (or clears, when the trimmed tag is empty) the caller's
// private label for a wallet, then returns the refreshed detail (TAG-H-01).
// Unknown wallets are WALLET-001; a missing tag field is COMMON-902.
func (s *Service) UpdateTag(ctx context.Context, userID, id uuid.UUID, rawTag *string) (*WalletDetail, error) {
	if rawTag == nil {
		return nil, domain.NewError(domain.ErrCodeValidation, "validation failed").
			WithDetails([]map[string]string{{
				"field": "tag", "code": string(domain.ErrCodeValidation),
				"message": "tag is required",
			}})
	}
	tag, clear, ferr := NormalizeTag(*rawTag)
	if ferr != nil {
		return nil, domain.NewError(domain.ErrCodeValidation, "validation failed").
			WithDetails([]map[string]string{{
				"field": ferr.Field, "code": ferr.Code, "message": ferr.Message,
			}})
	}

	exists, err := s.repo.WalletExists(ctx, id)
	if err != nil {
		return nil, domain.WrapError(domain.ErrCodeInternal, "wallet lookup failed", err)
	}
	if !exists {
		return nil, domain.NewError(domain.ErrCodeWalletNotFound, "wallet not found")
	}

	if clear {
		if err := s.repo.ClearTag(ctx, userID, id); err != nil {
			return nil, domain.WrapError(domain.ErrCodeInternal, "tag update failed", err)
		}
	} else if err := s.repo.UpsertTag(ctx, userID, id, tag); err != nil {
		return nil, domain.WrapError(domain.ErrCodeInternal, "tag update failed", err)
	}

	return s.Detail(ctx, userID, id, url.Values{})
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
