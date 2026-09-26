package walletgroup

import (
	"context"
	"errors"
	"math"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/qwerty7415963/go_be_arbitrage/internal/api"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

type RepositoryInterface interface {
	CreateGroup(ctx context.Context, g *Group) error
	GetGroupByID(ctx context.Context, id uuid.UUID) (*Group, error)
	ListGroups(ctx context.Context, userID uuid.UUID) ([]*Group, error)
	UpdateGroup(ctx context.Context, g *Group) error
	DeleteGroup(ctx context.Context, id, userID uuid.UUID) (int64, error)
	AddMembersTx(ctx context.Context, groupID, addedBy uuid.UUID, items []walletItem) (int64, error)
	RemoveMembers(ctx context.Context, groupID uuid.UUID, items []walletItem) (int64, error)
	ListMembers(ctx context.Context, groupID uuid.UUID, search string, limit, offset int) ([]*WalletRef, int64, error)
}

type Service struct {
	repo RepositoryInterface
}

func NewService(repo RepositoryInterface) *Service {
	return &Service{repo: repo}
}

// requireGroup loads the group and enforces ownership: unknown → GROUP-001,
// other user's group → GROUP-003.
func (s *Service) requireGroup(ctx context.Context, userID, groupID uuid.UUID) (*Group, error) {
	g, err := s.repo.GetGroupByID(ctx, groupID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.NewError(domain.ErrCodeGroupNotFound, "group not found")
		}
		return nil, domain.WrapError(domain.ErrCodeInternal, "failed to load group", err)
	}
	if g.UserID != userID {
		return nil, domain.NewError(domain.ErrCodeGroupForbidden, "you do not own this group")
	}
	return g, nil
}

func (s *Service) CreateGroup(ctx context.Context, userID uuid.UUID, req *CreateGroupRequest) (*Group, error) {
	name, fieldErr := ValidateGroupName(req.Name)
	if fieldErr != nil {
		return nil, domain.NewError(domain.ErrCodeValidation, "validation failed").
			WithDetails([]api.FieldError{*fieldErr})
	}

	g := &Group{
		ID:          uuid.New(),
		UserID:      userID,
		Name:        name,
		Description: req.Description,
		Color:       req.Color,
		WalletCount: 0,
	}

	if err := s.repo.CreateGroup(ctx, g); err != nil {
		if errors.Is(err, ErrDuplicateName) {
			return nil, domain.NewError(domain.ErrCodeGroupDuplicate, "group name already exists")
		}
		return nil, domain.WrapError(domain.ErrCodeInternal, "failed to create group", err)
	}
	return g, nil
}

func (s *Service) ListGroups(ctx context.Context, userID uuid.UUID) ([]*Group, error) {
	groups, err := s.repo.ListGroups(ctx, userID)
	if err != nil {
		return nil, domain.WrapError(domain.ErrCodeInternal, "failed to list groups", err)
	}
	return groups, nil
}

func (s *Service) GetGroup(ctx context.Context, userID, groupID uuid.UUID) (*Group, error) {
	return s.requireGroup(ctx, userID, groupID)
}

func (s *Service) UpdateGroup(ctx context.Context, userID, groupID uuid.UUID, req *UpdateGroupRequest) (*Group, error) {
	g, err := s.requireGroup(ctx, userID, groupID)
	if err != nil {
		return nil, err
	}

	if req.Name != nil {
		name, fieldErr := ValidateGroupName(*req.Name)
		if fieldErr != nil {
			return nil, domain.NewError(domain.ErrCodeValidation, "validation failed").
				WithDetails([]api.FieldError{*fieldErr})
		}
		g.Name = name
	}
	if req.Description != nil {
		g.Description = req.Description
	}
	if req.Color != nil {
		g.Color = req.Color
	}

	if err := s.repo.UpdateGroup(ctx, g); err != nil {
		if errors.Is(err, ErrDuplicateName) {
			return nil, domain.NewError(domain.ErrCodeGroupDuplicate, "group name already exists")
		}
		return nil, domain.WrapError(domain.ErrCodeInternal, "failed to update group", err)
	}
	return g, nil
}

// DeleteGroup removes the group; memberships cascade while tracked_wallets
// stay untouched (BR-10). Deleting an unknown or already deleted group →
// GROUP-001.
func (s *Service) DeleteGroup(ctx context.Context, userID, groupID uuid.UUID) error {
	g, err := s.requireGroup(ctx, userID, groupID)
	if err != nil {
		return err
	}

	rows, err := s.repo.DeleteGroup(ctx, g.ID, userID)
	if err != nil {
		return domain.WrapError(domain.ErrCodeInternal, "failed to delete group", err)
	}
	if rows == 0 {
		return domain.NewError(domain.ErrCodeGroupNotFound, "group not found")
	}
	return nil
}

func (s *Service) IsGroupOwnedBy(ctx context.Context, groupID, userID uuid.UUID) bool {
	g, err := s.repo.GetGroupByID(ctx, groupID)
	if err != nil {
		return false
	}
	return g.UserID == userID
}

// AddWallets resolves wallet IDs or raw addresses (normalized) and inserts
// memberships idempotently in one transaction (BR-09): unknown wallet ID →
// WALLET-001, invalid address → WALLET-002, and nothing is partially added.
func (s *Service) AddWallets(ctx context.Context, userID, groupID uuid.UUID, req *WalletsRequest) (int64, error) {
	if _, err := s.requireGroup(ctx, userID, groupID); err != nil {
		return 0, err
	}

	items, err := parseWalletItems(req.Wallets, req.Chain)
	if err != nil {
		return 0, err
	}

	created, err := s.repo.AddMembersTx(ctx, groupID, userID, items)
	if err != nil {
		if errors.Is(err, ErrWalletNotFound) {
			return 0, domain.NewError(domain.ErrCodeWalletNotFound, "wallet not found")
		}
		return 0, domain.WrapError(domain.ErrCodeInternal, "failed to add wallets", err)
	}
	return created, nil
}

// RemoveWallets is idempotent: wallets not in the group are a no-op.
func (s *Service) RemoveWallets(ctx context.Context, userID, groupID uuid.UUID, req *WalletsRequest) (int64, error) {
	if _, err := s.requireGroup(ctx, userID, groupID); err != nil {
		return 0, err
	}

	items, err := parseWalletItems(req.Wallets, req.Chain)
	if err != nil {
		return 0, err
	}

	removed, err := s.repo.RemoveMembers(ctx, groupID, items)
	if err != nil {
		return 0, domain.WrapError(domain.ErrCodeInternal, "failed to remove wallets", err)
	}
	return removed, nil
}

// ListGroupWallets enforces ownership, then returns one page of members and
// pagination metadata.
func (s *Service) ListGroupWallets(ctx context.Context, userID, groupID uuid.UUID, search string, page, limit int) ([]*WalletRef, *api.Meta, error) {
	if _, err := s.requireGroup(ctx, userID, groupID); err != nil {
		return nil, nil, err
	}

	page, limit = normalizePageLimit(page, limit)
	normalizedSearch := normalizeSearch(search)

	wallets, total, err := s.repo.ListMembers(ctx, groupID, normalizedSearch, limit, (page-1)*limit)
	if err != nil {
		return nil, nil, domain.WrapError(domain.ErrCodeInternal, "failed to list group wallets", err)
	}

	totalPages := int(math.Ceil(float64(total) / float64(limit)))
	return wallets, &api.Meta{
		Page:       page,
		TotalPages: totalPages,
		Limit:      limit,
		Offset:     (page - 1) * limit,
		HasMore:    int64(page*limit) < total,
	}, nil
}

func normalizePageLimit(page, limit int) (int, int) {
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

func normalizeSearch(search string) string {
	return strings.ToLower(strings.TrimSpace(search))
}
