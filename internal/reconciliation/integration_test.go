//go:build integration

package reconciliation

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const testDBURL = "postgres://test:test@localhost:5433/arbitrage_test?sslmode=disable"

func setupTestDB(t *testing.T) (*pgxpool.Pool, func()) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, testDBURL)
	if err != nil {
		t.Fatalf("failed to connect to test db: %v", err)
	}

	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("failed to ping test db: %v", err)
	}

	clean := func() {
		pool.Exec(context.Background(), "TRUNCATE reconciliation_items, reconciliation_runs, fills, orders, execution_legs, executions, risk_checks, risk_policies, strategy_decisions, strategy_instances, opportunity_legs, opportunities, unified_instruments, orderbook_snapshots, market_tickers, market_trades, funding_rates, venue_instruments, venue_accounts, instruments, venues, strategy_types, refresh_tokens, users, tenants CASCADE")
		pool.Close()
	}

	return pool, clean
}

func seedTestData(t *testing.T, pool *pgxpool.Pool) (tenantID, venueID, accountID uuid.UUID) {
	t.Helper()
	ctx := context.Background()

	tenantID = uuid.New()
	_, err := pool.Exec(ctx, "INSERT INTO tenants (id, name, status) VALUES ($1, 'Test Tenant', 'ACTIVE')",
		tenantID)
	if err != nil {
		t.Fatalf("failed to create tenant: %v", err)
	}

	venueID = uuid.New()
	venueCode := fmt.Sprintf("test-venue-%s", uuid.New().String()[:8])
	_, err = pool.Exec(ctx, "INSERT INTO venues (id, code, name, venue_type, status) VALUES ($1, $2, 'Test Venue', 'CEX', 'ACTIVE')",
		venueID, venueCode)
	if err != nil {
		t.Fatalf("failed to create venue: %v", err)
	}

	accountID = uuid.New()
	_, err = pool.Exec(ctx, "INSERT INTO venue_accounts (id, tenant_id, venue_id, account_name, status) VALUES ($1, $2, $3, 'test-account', 'ACTIVE')",
		accountID, tenantID, venueID)
	if err != nil {
		t.Fatalf("failed to create venue account: %v", err)
	}

	return tenantID, venueID, accountID
}

func TestIntegrationRepository_CreateRun(t *testing.T) {
	if os.Getenv("INTEGRATION") != "1" {
		t.Skip("set INTEGRATION=1 to run")
	}

	pool, clean := setupTestDB(t)
	defer clean()

	tenantID, _, accountID := seedTestData(t, pool)

	repo := NewRepository(pool)
	ctx := context.Background()

	run := &ReconciliationRun{
		ID:             uuid.New(),
		TenantID:       tenantID,
		VenueAccountID: accountID,
		TriggerSource:  TriggerScheduled,
		Status:         RunStatusRunning,
		StartedAt:      time.Now(),
		Summary:        []byte(`{}`),
	}

	err := repo.CreateRun(ctx, run)
	if err != nil {
		t.Fatalf("failed to create run: %v", err)
	}

	got, err := repo.GetRunByID(ctx, run.ID)
	if err != nil {
		t.Fatalf("failed to get run: %v", err)
	}

	if got.TriggerSource != TriggerScheduled {
		t.Errorf("expected SCHEDULED, got %s", got.TriggerSource)
	}
	if got.Status != RunStatusRunning {
		t.Errorf("expected RUNNING, got %s", got.Status)
	}
}

func TestIntegrationRepository_CreateItem(t *testing.T) {
	if os.Getenv("INTEGRATION") != "1" {
		t.Skip("set INTEGRATION=1 to run")
	}

	pool, clean := setupTestDB(t)
	defer clean()

	tenantID, _, accountID := seedTestData(t, pool)

	repo := NewRepository(pool)
	ctx := context.Background()

	run := &ReconciliationRun{
		ID:             uuid.New(),
		TenantID:       tenantID,
		VenueAccountID: accountID,
		TriggerSource:  TriggerManual,
		Status:         RunStatusRunning,
		StartedAt:      time.Now(),
		Summary:        []byte(`{}`),
	}
	repo.CreateRun(ctx, run)

	item := &ReconciliationItem{
		ID:            uuid.New(),
		RunID:         run.ID,
		EntityType:    EntityBalance,
		Result:        ResultMatch,
		InternalState: []byte(`{"asset":"BTC","amount":1.0}`),
		ExternalState: []byte(`{"asset":"BTC","amount":1.0}`),
		CreatedAt:     time.Now(),
	}

	err := repo.CreateItem(ctx, item)
	if err != nil {
		t.Fatalf("failed to create item: %v", err)
	}

	items, err := repo.ListItemsByRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("failed to list items: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].Result != ResultMatch {
		t.Errorf("expected MATCH, got %s", items[0].Result)
	}
}

func TestIntegrationRepository_ListRunsByTenant(t *testing.T) {
	if os.Getenv("INTEGRATION") != "1" {
		t.Skip("set INTEGRATION=1 to run")
	}

	pool, clean := setupTestDB(t)
	defer clean()

	tenantID, _, accountID := seedTestData(t, pool)

	repo := NewRepository(pool)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		run := &ReconciliationRun{
			ID:             uuid.New(),
			TenantID:       tenantID,
			VenueAccountID: accountID,
			TriggerSource:  TriggerScheduled,
			Status:         RunStatusRunning,
			StartedAt:      time.Now(),
			Summary:        []byte(`{}`),
		}
		repo.CreateRun(ctx, run)
	}

	runs, err := repo.ListRunsByTenant(ctx, tenantID, 10, 0)
	if err != nil {
		t.Fatalf("failed to list runs: %v", err)
	}
	if len(runs) != 3 {
		t.Errorf("expected 3 runs, got %d", len(runs))
	}
}

func TestIntegrationRepository_ListItemsByRun(t *testing.T) {
	if os.Getenv("INTEGRATION") != "1" {
		t.Skip("set INTEGRATION=1 to run")
	}

	pool, clean := setupTestDB(t)
	defer clean()

	tenantID, _, accountID := seedTestData(t, pool)

	repo := NewRepository(pool)
	ctx := context.Background()

	run := &ReconciliationRun{
		ID:             uuid.New(),
		TenantID:       tenantID,
		VenueAccountID: accountID,
		TriggerSource:  TriggerStartup,
		Status:         RunStatusRunning,
		StartedAt:      time.Now(),
		Summary:        []byte(`{}`),
	}
	repo.CreateRun(ctx, run)

	for _, entityType := range []EntityType{EntityBalance, EntityPosition, EntityMargin} {
		item := &ReconciliationItem{
			ID:            uuid.New(),
			RunID:         run.ID,
			EntityType:    entityType,
			Result:        ResultMatch,
			InternalState: []byte(`{}`),
			ExternalState: []byte(`{}`),
			CreatedAt:     time.Now(),
		}
		repo.CreateItem(ctx, item)
	}

	items, err := repo.ListItemsByRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("failed to list items: %v", err)
	}
	if len(items) != 3 {
		t.Errorf("expected 3 items, got %d", len(items))
	}
}

func TestIntegrationRepository_UpdateRunStatus(t *testing.T) {
	if os.Getenv("INTEGRATION") != "1" {
		t.Skip("set INTEGRATION=1 to run")
	}

	pool, clean := setupTestDB(t)
	defer clean()

	tenantID, _, accountID := seedTestData(t, pool)

	repo := NewRepository(pool)
	ctx := context.Background()

	run := &ReconciliationRun{
		ID:             uuid.New(),
		TenantID:       tenantID,
		VenueAccountID: accountID,
		TriggerSource:  TriggerReconnect,
		Status:         RunStatusRunning,
		StartedAt:      time.Now(),
		Summary:        []byte(`{}`),
	}
	repo.CreateRun(ctx, run)

	summary := []byte(`{"total_items":2,"matched":2}`)
	err := repo.UpdateRunStatus(ctx, run.ID, RunStatusMatched, summary)
	if err != nil {
		t.Fatalf("failed to update run status: %v", err)
	}

	got, err := repo.GetRunByID(ctx, run.ID)
	if err != nil {
		t.Fatalf("failed to get run: %v", err)
	}
	if got.Status != RunStatusMatched {
		t.Errorf("expected MATCHED, got %s", got.Status)
	}
	if got.CompletedAt == nil {
		t.Error("expected completed_at to be set")
	}
}
