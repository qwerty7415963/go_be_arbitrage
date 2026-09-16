package reconciliation

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

type mockExchange struct {
	balance    *BalanceState
	position   *PositionState
	orders     []*OrderState
	fills      []*FillState
	margin     *MarginState
	balanceErr error
	posErr     error
	ordersErr  error
	fillsErr   error
	marginErr  error
}

func (m *mockExchange) GetBalance(ctx context.Context, asset string) (float64, float64, error) {
	if m.balanceErr != nil || m.balance == nil {
		return 0, 0, m.balanceErr
	}
	return m.balance.Amount, m.balance.Reserved, nil
}

func (m *mockExchange) GetPosition(ctx context.Context, symbol string) (*PositionState, error) {
	if m.posErr != nil || m.position == nil {
		return nil, m.posErr
	}
	return m.position, nil
}

func (m *mockExchange) GetOpenOrders(ctx context.Context) ([]*OrderState, error) {
	if m.ordersErr != nil {
		return nil, m.ordersErr
	}
	return m.orders, nil
}

func (m *mockExchange) GetFills(ctx context.Context, since time.Time) ([]*FillState, error) {
	if m.fillsErr != nil {
		return nil, m.fillsErr
	}
	return m.fills, nil
}

func (m *mockExchange) GetMarginState(ctx context.Context) (*MarginState, error) {
	if m.marginErr != nil || m.margin == nil {
		return nil, m.marginErr
	}
	return m.margin, nil
}

type mockRepoStore struct {
	runs  map[uuid.UUID]*ReconciliationRun
	items map[uuid.UUID][]*ReconciliationItem
}

func newMockRepoStore() *mockRepoStore {
	return &mockRepoStore{
		runs:  make(map[uuid.UUID]*ReconciliationRun),
		items: make(map[uuid.UUID][]*ReconciliationItem),
	}
}

func (m *mockRepoStore) saveRun(run *ReconciliationRun) {
	m.runs[run.ID] = run
}

func (m *mockRepoStore) getRun(id uuid.UUID) (*ReconciliationRun, bool) {
	r, ok := m.runs[id]
	return r, ok
}

func (m *mockRepoStore) updateRunStatus(id uuid.UUID, status RunStatus, summary json.RawMessage) {
	if r, ok := m.runs[id]; ok {
		r.Status = status
		r.Summary = summary
		now := time.Now()
		r.CompletedAt = &now
	}
}

func (m *mockRepoStore) saveItem(item *ReconciliationItem) {
	m.items[item.RunID] = append(m.items[item.RunID], item)
}

func (m *mockRepoStore) listItems(runID uuid.UUID) []*ReconciliationItem {
	return m.items[runID]
}

func TestEngine_StartReconciliation(t *testing.T) {
	store := newMockRepoStore()
	tenantID := uuid.New()
	accountID := uuid.New()

	run := &ReconciliationRun{
		ID:             uuid.New(),
		TenantID:       tenantID,
		VenueAccountID: accountID,
		TriggerSource:  TriggerScheduled,
		Status:         RunStatusRunning,
		StartedAt:      time.Now(),
		Summary:        json.RawMessage(`{}`),
	}
	store.saveRun(run)

	got, ok := store.getRun(run.ID)
	if !ok {
		t.Fatal("run not found")
	}
	if got.Status != RunStatusRunning {
		t.Errorf("expected RUNNING, got %s", got.Status)
	}
	if got.TriggerSource != TriggerScheduled {
		t.Errorf("expected SCHEDULED, got %s", got.TriggerSource)
	}
}

func TestEngine_AllMatch(t *testing.T) {
	c := NewComparator()
	items := []*ReconciliationItem{
		{Result: ResultMatch},
		{Result: ResultMatch},
		{Result: ResultMatch},
	}
	summary := c.SummarizeItems(items)
	status := c.OverallStatus(summary)
	if status != RunStatusMatched {
		t.Errorf("expected MATCHED, got %s", status)
	}
	if summary.Matched != 3 {
		t.Errorf("expected 3 matched, got %d", summary.Matched)
	}
}

func TestEngine_SomeMismatch(t *testing.T) {
	c := NewComparator()
	items := []*ReconciliationItem{
		{Result: ResultMatch},
		{Result: ResultMismatch},
		{Result: ResultMissingExternal},
	}
	summary := c.SummarizeItems(items)
	status := c.OverallStatus(summary)
	if status != RunStatusMismatch {
		t.Errorf("expected MISMATCH, got %s", status)
	}
	if summary.Mismatched != 1 {
		t.Errorf("expected 1 mismatched, got %d", summary.Mismatched)
	}
}

func TestEngine_MockStore_CreateAndGetRun(t *testing.T) {
	store := newMockRepoStore()

	run := &ReconciliationRun{
		ID:             uuid.New(),
		TenantID:       uuid.New(),
		VenueAccountID: uuid.New(),
		TriggerSource:  TriggerStartup,
		Status:         RunStatusRunning,
		StartedAt:      time.Now(),
		Summary:        json.RawMessage(`{}`),
	}
	store.saveRun(run)

	got, ok := store.getRun(run.ID)
	if !ok {
		t.Fatal("run not found")
	}
	if got.TriggerSource != TriggerStartup {
		t.Errorf("expected STARTUP, got %s", got.TriggerSource)
	}
}

func TestEngine_MockStore_UpdateRunStatus(t *testing.T) {
	store := newMockRepoStore()

	run := &ReconciliationRun{
		ID:             uuid.New(),
		TenantID:       uuid.New(),
		VenueAccountID: uuid.New(),
		TriggerSource:  TriggerManual,
		Status:         RunStatusRunning,
		StartedAt:      time.Now(),
		Summary:        json.RawMessage(`{}`),
	}
	store.saveRun(run)

	summary := json.RawMessage(`{"total_items":3,"matched":3}`)
	store.updateRunStatus(run.ID, RunStatusMatched, summary)

	got, _ := store.getRun(run.ID)
	if got.Status != RunStatusMatched {
		t.Errorf("expected MATCHED, got %s", got.Status)
	}
	if got.CompletedAt == nil {
		t.Error("expected completed_at to be set")
	}
}

func TestEngine_MockStore_CreateAndListItems(t *testing.T) {
	store := newMockRepoStore()
	runID := uuid.New()

	item1 := &ReconciliationItem{
		ID:     uuid.New(),
		RunID:  runID,
		Result: ResultMatch,
	}
	item2 := &ReconciliationItem{
		ID:     uuid.New(),
		RunID:  runID,
		Result: ResultMismatch,
	}

	store.saveItem(item1)
	store.saveItem(item2)

	items := store.listItems(runID)
	if len(items) != 2 {
		t.Errorf("expected 2 items, got %d", len(items))
	}
}

func TestEngine_MockExchange_GetBalance(t *testing.T) {
	ex := &mockExchange{
		balance: &BalanceState{Asset: "BTC", Amount: 1.5, Reserved: 0.1},
	}
	amt, res, err := ex.GetBalance(context.Background(), "BTC")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if amt != 1.5 {
		t.Errorf("expected 1.5, got %f", amt)
	}
	if res != 0.1 {
		t.Errorf("expected 0.1, got %f", res)
	}
}

func TestEngine_MockExchange_GetMarginState(t *testing.T) {
	ex := &mockExchange{
		margin: &MarginState{TotalEquity: 10000, UsedMargin: 2000, AvailableMargin: 8000, MarginRatio: 0.2},
	}
	m, err := ex.GetMarginState(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.UsedMargin != 2000 {
		t.Errorf("expected 2000, got %f", m.UsedMargin)
	}
}
