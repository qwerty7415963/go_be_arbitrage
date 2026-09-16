package reconciliation

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type ExchangeAdapter interface {
	GetBalance(ctx context.Context, asset string) (float64, float64, error)
	GetPosition(ctx context.Context, symbol string) (*PositionState, error)
	GetOpenOrders(ctx context.Context) ([]*OrderState, error)
	GetFills(ctx context.Context, since time.Time) ([]*FillState, error)
	GetMarginState(ctx context.Context) (*MarginState, error)
}

type Engine struct {
	repo       *Repository
	comparator *Comparator
}

func NewEngine(repo *Repository) *Engine {
	return &Engine{
		repo:       repo,
		comparator: NewComparator(),
	}
}

func (e *Engine) StartReconciliation(ctx context.Context, tenantID, venueAccountID uuid.UUID, trigger TriggerSource) (*ReconciliationRun, error) {
	run := &ReconciliationRun{
		ID:             uuid.New(),
		TenantID:       tenantID,
		VenueAccountID: venueAccountID,
		TriggerSource:  trigger,
		Status:         RunStatusRunning,
		StartedAt:      time.Now(),
		Summary:        json.RawMessage(`{}`),
	}

	if err := e.repo.CreateRun(ctx, run); err != nil {
		return nil, fmt.Errorf("create run: %w", err)
	}

	return run, nil
}

func (e *Engine) RunChecks(ctx context.Context, runID uuid.UUID, adapter ExchangeAdapter, internalBalances []*BalanceState, internalPositions []*PositionState, internalOrders []*OrderState, internalFills []*FillState, internalMargin *MarginState) error {
	run, err := e.repo.GetRunByID(ctx, runID)
	if err != nil {
		return fmt.Errorf("get run: %w", err)
	}
	if run.Status != RunStatusRunning {
		return ErrRunNotRunning
	}

	var items []*ReconciliationItem

	extMargin, err := adapter.GetMarginState(ctx)
	if err == nil {
		item := e.comparator.CompareMargin(internalMargin, extMargin)
		item.RunID = runID
		item.ID = uuid.New()
		item.CreatedAt = time.Now()
		items = append(items, item)
	}

	for _, intBal := range internalBalances {
		extAmount, extReserved, err := adapter.GetBalance(ctx, intBal.Asset)
		if err != nil {
			continue
		}
		item := e.comparator.CompareBalance(intBal, &BalanceState{
			Asset:    intBal.Asset,
			Amount:   extAmount,
			Reserved: extReserved,
		})
		item.RunID = runID
		item.ID = uuid.New()
		item.CreatedAt = time.Now()
		items = append(items, item)
	}

	for _, intPos := range internalPositions {
		extPos, err := adapter.GetPosition(ctx, intPos.Symbol)
		if err != nil {
			continue
		}
		item := e.comparator.ComparePosition(intPos, extPos)
		item.RunID = runID
		item.ID = uuid.New()
		item.CreatedAt = time.Now()
		items = append(items, item)
	}

	extOrders, err := adapter.GetOpenOrders(ctx)
	if err == nil {
		extOrderMap := make(map[string]*OrderState)
		for _, o := range extOrders {
			extOrderMap[o.OrderID] = o
		}
		for _, intOrd := range internalOrders {
			extOrd := extOrderMap[intOrd.OrderID]
			item := e.comparator.CompareOrder(intOrd, extOrd)
			item.RunID = runID
			item.ID = uuid.New()
			item.CreatedAt = time.Now()
			items = append(items, item)
		}
	}

	extFills, err := adapter.GetFills(ctx, run.StartedAt)
	if err == nil {
		item := e.comparator.CompareFills(internalFills, extFills)
		item.RunID = runID
		item.ID = uuid.New()
		item.CreatedAt = time.Now()
		items = append(items, item)
	}

	for _, item := range items {
		if err := e.repo.CreateItem(ctx, item); err != nil {
			return fmt.Errorf("create item: %w", err)
		}
	}

	summary := e.comparator.SummarizeItems(items)
	status := e.comparator.OverallStatus(summary)
	summaryJSON := e.comparator.SummaryToJSON(summary)

	if err := e.repo.UpdateRunStatus(ctx, runID, status, summaryJSON); err != nil {
		return fmt.Errorf("update run status: %w", err)
	}

	return nil
}

func (e *Engine) GetRun(ctx context.Context, id uuid.UUID) (*ReconciliationRun, error) {
	return e.repo.GetRunByID(ctx, id)
}

func (e *Engine) ListRuns(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]*ReconciliationRun, error) {
	return e.repo.ListRunsByTenant(ctx, tenantID, limit, offset)
}

func (e *Engine) ListItems(ctx context.Context, runID uuid.UUID) ([]*ReconciliationItem, error) {
	return e.repo.ListItemsByRun(ctx, runID)
}
