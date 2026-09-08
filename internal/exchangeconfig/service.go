package exchangeconfig

import (
	"context"

	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) Create(ctx context.Context, config *ExchangeConfig) error {
	exists, err := s.repo.ExistsByVenueID(ctx, config.VenueID)
	if err != nil {
		return domain.WrapError(domain.ErrCodeInternal, "failed to check venue config", err)
	}
	if exists {
		return domain.NewError(domain.ErrCodeExchangeConfigDuplicate, "exchange config already exists for this venue")
	}

	if config.RateLimitRPM == 0 {
		config.RateLimitRPM = 1200
	}
	if config.TimeoutMs == 0 {
		config.TimeoutMs = 5000
	}
	if config.Status == "" {
		config.Status = ExchangeConfigStatusActive
	}

	return s.repo.Create(ctx, config)
}

func (s *Service) GetByID(ctx context.Context, id uuid.UUID) (*ExchangeConfig, error) {
	config, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, domain.NewError(domain.ErrCodeExchangeConfigNotFound, "exchange config not found")
	}
	return config, nil
}

func (s *Service) GetByVenueID(ctx context.Context, venueID uuid.UUID) (*ExchangeConfig, error) {
	config, err := s.repo.GetByVenueID(ctx, venueID)
	if err != nil {
		return nil, domain.NewError(domain.ErrCodeExchangeConfigNotFound, "exchange config not found for venue")
	}
	return config, nil
}

func (s *Service) List(ctx context.Context, limit, offset int) ([]*ExchangeConfig, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	return s.repo.List(ctx, limit, offset)
}

func (s *Service) Update(ctx context.Context, id uuid.UUID, req *UpdateExchangeConfigRequest) (*ExchangeConfig, error) {
	config, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, domain.NewError(domain.ErrCodeExchangeConfigNotFound, "exchange config not found")
	}

	if req.ExchangeName != nil {
		config.ExchangeName = *req.ExchangeName
	}
	if req.RestBaseURL != nil {
		config.RestBaseURL = *req.RestBaseURL
	}
	if req.WsURL != nil {
		config.WsURL = *req.WsURL
	}
	if req.Status != nil {
		config.Status = *req.Status
	}
	if req.RateLimitRPM != nil {
		config.RateLimitRPM = *req.RateLimitRPM
	}
	if req.TimeoutMs != nil {
		config.TimeoutMs = *req.TimeoutMs
	}

	if err := s.repo.Update(ctx, id, config); err != nil {
		return nil, domain.WrapError(domain.ErrCodeInternal, "failed to update exchange config", err)
	}

	return config, nil
}

func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return domain.NewError(domain.ErrCodeExchangeConfigNotFound, "exchange config not found")
	}

	return s.repo.Delete(ctx, id)
}
