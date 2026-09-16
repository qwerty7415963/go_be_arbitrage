package execution

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

type Service struct {
	repo   *Repository
	engine *Engine
}

func NewService(repo *Repository) *Service {
	return &Service{
		repo:   repo,
		engine: NewEngine(repo),
	}
}

func (s *Service) CreateExecution(ctx context.Context, tenantID uuid.UUID, req *CreateExecutionRequest) (*Execution, error) {
	return s.engine.CreateExecution(ctx, tenantID, req)
}

func (s *Service) SubmitExecution(ctx context.Context, id uuid.UUID) error {
	return s.engine.SubmitExecution(ctx, id)
}

func (s *Service) CancelExecution(ctx context.Context, id uuid.UUID) error {
	return s.engine.CancelExecution(ctx, id)
}

func (s *Service) GetExecution(ctx context.Context, id uuid.UUID) (*Execution, error) {
	return s.engine.GetExecution(ctx, id)
}

func (s *Service) ListLegs(ctx context.Context, executionID uuid.UUID) ([]*ExecutionLeg, error) {
	return s.repo.ListLegsByExecution(ctx, executionID)
}

func (s *Service) CreateOrder(ctx context.Context, order *Order) error {
	return s.repo.CreateOrder(ctx, order)
}

func (s *Service) GetOrder(ctx context.Context, venueAccountID uuid.UUID, clientOrderID string) (*Order, error) {
	return s.repo.GetOrderByClientOrderID(ctx, venueAccountID, clientOrderID)
}

func (s *Service) CreateFill(ctx context.Context, fill *Fill) error {
	if err := s.repo.CreateFill(ctx, fill); err != nil {
		return fmt.Errorf("create fill: %w", err)
	}
	return nil
}

func (s *Service) ListFills(ctx context.Context, orderID uuid.UUID) ([]*Fill, error) {
	return s.repo.ListFillsByOrder(ctx, orderID)
}
