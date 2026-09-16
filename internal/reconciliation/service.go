package reconciliation

import (
	"context"

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

func (s *Service) StartReconciliation(ctx context.Context, tenantID, venueAccountID uuid.UUID, trigger TriggerSource) (*ReconciliationRun, error) {
	return s.engine.StartReconciliation(ctx, tenantID, venueAccountID, trigger)
}

func (s *Service) GetRun(ctx context.Context, id uuid.UUID) (*ReconciliationRun, error) {
	return s.engine.GetRun(ctx, id)
}

func (s *Service) ListRuns(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]*ReconciliationRun, error) {
	return s.engine.ListRuns(ctx, tenantID, limit, offset)
}

func (s *Service) ListItems(ctx context.Context, runID uuid.UUID) ([]*ReconciliationItem, error) {
	return s.engine.ListItems(ctx, runID)
}

func (s *Service) RunChecks(ctx context.Context, runID uuid.UUID, adapter ExchangeAdapter, internalBalances []*BalanceState, internalPositions []*PositionState, internalOrders []*OrderState, internalFills []*FillState, internalMargin *MarginState) error {
	return s.engine.RunChecks(ctx, runID, adapter, internalBalances, internalPositions, internalOrders, internalFills, internalMargin)
}
