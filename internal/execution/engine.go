package execution

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Engine struct {
	repo     *Repository
	policy   *PolicySelector
	running  map[uuid.UUID]context.CancelFunc
}

func NewEngine(repo *Repository) *Engine {
	return &Engine{
		repo:    repo,
		policy:  NewPolicySelector(),
		running: make(map[uuid.UUID]context.CancelFunc),
	}
}

func (e *Engine) CreateExecution(ctx context.Context, tenantID uuid.UUID, req *CreateExecutionRequest) (*Execution, error) {
	exec := &Execution{
		ID:                 uuid.New(),
		TenantID:           tenantID,
		StrategyInstanceID: req.StrategyInstanceID,
		ExecutionMode:      req.ExecutionMode,
		Status:             ExecutionStatusCreated,
		IntentJSON:         req.Intent,
		CreatedAt:          time.Now(),
	}

	if err := e.repo.CreateExecution(ctx, exec); err != nil {
		return nil, fmt.Errorf("create execution: %w", err)
	}

	for i, legReq := range req.Intent.Legs {
		leg := &ExecutionLeg{
			ID:             uuid.New(),
			ExecutionID:    exec.ID,
			LegIndex:       i,
			LegRole:        "PRIMARY",
			VenueAccountID: legReq.VenueAccountID,
			InstrumentID:   legReq.InstrumentID,
			TargetSide:     legReq.Side,
			TargetQuantity: &legReq.Quantity,
			TargetNotional: &legReq.Price,
			Status:         LegStatusPending,
			Metadata:       map[string]interface{}{},
		}
		if err := e.repo.CreateLeg(ctx, leg); err != nil {
			return nil, fmt.Errorf("create leg: %w", err)
		}
	}

	return exec, nil
}

func (e *Engine) SubmitExecution(ctx context.Context, execID uuid.UUID) error {
	exec, err := e.repo.GetExecutionByID(ctx, execID)
	if err != nil {
		return err
	}

	if exec.ExecutionMode == ExecutionModePaper {
		return e.executePaper(ctx, exec)
	}

	return e.repo.UpdateExecutionStatus(ctx, execID, ExecutionStatusSubmitting)
}

func (e *Engine) executePaper(ctx context.Context, exec *Execution) error {
	if err := e.repo.UpdateExecutionStatus(ctx, exec.ID, ExecutionStatusSubmitting); err != nil {
		return err
	}

	legs, err := e.repo.ListLegsByExecution(ctx, exec.ID)
	if err != nil {
		return err
	}

	for _, leg := range legs {
		order := &Order{
			ID:                uuid.New(),
			ExecutionLegID:    &leg.ID,
			VenueAccountID:    leg.VenueAccountID,
			InstrumentID:      leg.InstrumentID,
			ClientOrderID:     fmt.Sprintf("paper-%s-%d", exec.ID.String()[:8], leg.LegIndex),
			Side:              leg.TargetSide,
			OrderType:         "LIMIT",
			RequestedQuantity: leg.TargetQuantity,
			RequestedPrice:    leg.TargetNotional,
			Status:            OrderStatusCreated,
			CreatedAt:         time.Now(),
			UpdatedAt:         time.Now(),
		}

		if err := e.repo.CreateOrder(ctx, order); err != nil {
			return err
		}

		if err := TransitionOrder(order, OrderStatusSubmitting); err != nil {
			return err
		}
		if err := e.repo.UpdateOrderStatus(ctx, order.ID, OrderStatusSubmitting); err != nil {
			return err
		}

		if err := TransitionOrder(order, OrderStatusFilled); err != nil {
			return err
		}
		if err := e.repo.UpdateOrderStatus(ctx, order.ID, OrderStatusFilled); err != nil {
			return err
		}
	}

	return e.repo.UpdateExecutionStatus(ctx, exec.ID, ExecutionStatusCompleted)
}

func (e *Engine) CancelExecution(ctx context.Context, execID uuid.UUID) error {
	return e.repo.UpdateExecutionStatus(ctx, execID, ExecutionStatusCanceled)
}

func (e *Engine) GetExecution(ctx context.Context, id uuid.UUID) (*Execution, error) {
	return e.repo.GetExecutionByID(ctx, id)
}
