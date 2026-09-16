package strategy

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/opportunity"
)

type Service struct {
	repo   *Repository
	engine *Engine
	oppSvc *opportunity.Service
}

func NewService(repo *Repository, oppSvc *opportunity.Service) *Service {
	return &Service{
		repo:   repo,
		engine: NewEngine(),
		oppSvc: oppSvc,
	}
}

func (s *Service) Create(ctx context.Context, req *CreateStrategyRequest, tenantID uuid.UUID) (*StrategyInstance, error) {
	strategyTypeID, err := s.repo.GetStrategyTypeByCode(ctx, string(req.Type))
	if err != nil {
		return nil, fmt.Errorf("strategy type not found: %w", err)
	}

	instance := &StrategyInstance{
		ID:             uuid.New(),
		TenantID:       tenantID,
		StrategyTypeID: strategyTypeID,
		Name:           req.Name,
		Mode:           req.Mode,
		Status:         StrategyStatusDraft,
		Config:         req.Config,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	if err := s.repo.Create(ctx, instance); err != nil {
		return nil, fmt.Errorf("create strategy: %w", err)
	}

	return instance, nil
}

func (s *Service) GetByID(ctx context.Context, id uuid.UUID) (*StrategyInstance, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *Service) List(ctx context.Context, tenantID uuid.UUID) ([]*StrategyInstance, error) {
	return s.repo.List(ctx, tenantID)
}

func (s *Service) Update(ctx context.Context, id uuid.UUID, req *UpdateStrategyRequest) (*StrategyInstance, error) {
	instance, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if req.Name != nil {
		instance.Name = *req.Name
	}
	if req.Status != nil {
		instance.Status = *req.Status
	}
	if req.Config != nil {
		instance.Config = req.Config
	}
	instance.UpdatedAt = time.Now()

	if err := s.repo.Update(ctx, instance); err != nil {
		return nil, fmt.Errorf("update strategy: %w", err)
	}

	return instance, nil
}

func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	s.engine.StopInstance(id)
	return s.repo.Delete(ctx, id)
}

func (s *Service) Start(ctx context.Context, id uuid.UUID) error {
	instance, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}

	if instance.Status == StrategyStatusRunning {
		return fmt.Errorf("strategy already active")
	}

	instance.Status = StrategyStatusRunning
	instance.UpdatedAt = time.Now()

	if err := s.repo.Update(ctx, instance); err != nil {
		return fmt.Errorf("update strategy status: %w", err)
	}

	s.engine.StartInstance(ctx, instance, s.oppSvc)
	return nil
}

func (s *Service) Stop(ctx context.Context, id uuid.UUID) error {
	instance, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}

	instance.Status = StrategyStatusPaused
	instance.UpdatedAt = time.Now()

	if err := s.repo.Update(ctx, instance); err != nil {
		return fmt.Errorf("update strategy status: %w", err)
	}

	s.engine.StopInstance(id)
	return nil
}

func (s *Service) GetDecisions(ctx context.Context, strategyID uuid.UUID, limit int) ([]*StrategyDecision, error) {
	return s.repo.ListDecisions(ctx, strategyID, limit)
}
