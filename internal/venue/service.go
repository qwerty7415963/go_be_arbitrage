package venue

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

type RepositoryInterface interface {
	Create(ctx context.Context, v *Venue) error
	GetByID(ctx context.Context, id uuid.UUID) (*Venue, error)
	GetByCode(ctx context.Context, code string) (*Venue, error)
	List(ctx context.Context) ([]*Venue, error)
	Update(ctx context.Context, v *Venue) error
	Delete(ctx context.Context, id uuid.UUID) error
}

type Service struct {
	repo RepositoryInterface
}

func NewService(repo RepositoryInterface) *Service {
	return &Service{repo: repo}
}

func (s *Service) Create(ctx context.Context, req *CreateVenueRequest) (*Venue, error) {
	existing, _ := s.repo.GetByCode(ctx, req.Code)
	if existing != nil {
		return nil, domain.NewError(domain.ErrCodeConfigInvalid, "venue code already exists")
	}

	caps, err := json.Marshal(req.Capabilities)
	if err != nil {
		return nil, fmt.Errorf("marshal capabilities: %w", err)
	}

	v := &Venue{
		ID:           uuid.New(),
		Code:         req.Code,
		Name:         req.Name,
		VenueType:    req.VenueType,
		Status:       VenueStatusActive,
		Capabilities: caps,
		Metadata:     json.RawMessage(`{}`),
	}

	if err := s.repo.Create(ctx, v); err != nil {
		return nil, domain.WrapError(domain.ErrCodeConfigInvalid, "failed to create venue", err)
	}

	return v, nil
}

func (s *Service) GetByID(ctx context.Context, id uuid.UUID) (*Venue, error) {
	v, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, domain.WrapError(domain.ErrCodeConfigNotFound, "venue not found", err)
	}
	return v, nil
}

func (s *Service) GetByCode(ctx context.Context, code string) (*Venue, error) {
	v, err := s.repo.GetByCode(ctx, code)
	if err != nil {
		return nil, domain.WrapError(domain.ErrCodeConfigNotFound, "venue not found", err)
	}
	return v, nil
}

func (s *Service) List(ctx context.Context) ([]*Venue, error) {
	return s.repo.List(ctx)
}

func (s *Service) Update(ctx context.Context, id uuid.UUID, req *UpdateVenueRequest) (*Venue, error) {
	v, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, domain.WrapError(domain.ErrCodeConfigNotFound, "venue not found", err)
	}

	if req.Name != nil {
		v.Name = *req.Name
	}
	if req.Status != nil {
		v.Status = *req.Status
	}
	if req.Capabilities != nil {
		caps, err := json.Marshal(req.Capabilities)
		if err != nil {
			return nil, fmt.Errorf("marshal capabilities: %w", err)
		}
		v.Capabilities = caps
	}

	if err := s.repo.Update(ctx, v); err != nil {
		return nil, domain.WrapError(domain.ErrCodeConfigInvalid, "failed to update venue", err)
	}

	return v, nil
}

func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return domain.WrapError(domain.ErrCodeConfigNotFound, "venue not found", err)
	}

	return s.repo.Delete(ctx, id)
}
