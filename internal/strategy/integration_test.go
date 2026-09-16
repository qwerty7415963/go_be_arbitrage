//go:build integration

package strategy

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/qwerty7415963/go_be_arbitrage/internal/opportunity"
)

func setupTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dbHost := os.Getenv("ARBITRAGE_DB_HOST")
	if dbHost == "" {
		dbHost = "localhost"
	}
	dbPort := os.Getenv("ARBITRAGE_DB_PORT")
	if dbPort == "" {
		dbPort = "5433"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	dsn := "postgres://test:test@" + dbHost + ":" + dbPort + "/arbitrage_test?sslmode=disable"

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("failed to connect to database: %v", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("failed to ping database: %v", err)
	}

	// Clean up tables before test
	pool.Exec(ctx, "DELETE FROM strategy_decisions")
	pool.Exec(ctx, "DELETE FROM strategy_instances")

	// Ensure system tenant exists
	var tenantID uuid.UUID
	err = pool.QueryRow(ctx, "SELECT id FROM tenants LIMIT 1").Scan(&tenantID)
	if err != nil {
		tenantID = uuid.New()
		_, err = pool.Exec(ctx, "INSERT INTO tenants (id, name, status) VALUES ($1, 'system', 'ACTIVE')", tenantID)
		if err != nil {
			t.Fatalf("failed to create system tenant: %v", err)
		}
	}

	// Ensure strategy types exist
	for _, st := range []struct{ code, name string }{
		{"PRICE_ARBITRAGE", "Price Arbitrage"},
		{"FUNDING_ARBITRAGE", "Funding Arbitrage"},
		{"BASIS_ARBITRAGE", "Basis Arbitrage"},
	} {
		var exists bool
		err = pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM strategy_types WHERE code = $1)", st.code).Scan(&exists)
		if err != nil || !exists {
			_, err = pool.Exec(ctx, "INSERT INTO strategy_types (id, code, name, status) VALUES ($1, $2, $3, 'ACTIVE')",
				uuid.New(), st.code, st.name)
			if err != nil {
				t.Fatalf("failed to create strategy type %s: %v", st.code, err)
			}
		}
	}

	t.Setenv("TEST_TENANT_ID", tenantID.String())

	return pool
}

func getTestTenantID(t *testing.T) uuid.UUID {
	t.Helper()
	tenantIDStr := os.Getenv("TEST_TENANT_ID")
	if tenantIDStr == "" {
		t.Fatal("TEST_TENANT_ID not set")
	}
	id, err := uuid.Parse(tenantIDStr)
	if err != nil {
		t.Fatalf("invalid TEST_TENANT_ID: %v", err)
	}
	return id
}

func getStrategyTypeID(t *testing.T, pool *pgxpool.Pool, code string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := pool.QueryRow(context.Background(), "SELECT id FROM strategy_types WHERE code = $1", code).Scan(&id)
	if err != nil {
		t.Fatalf("failed to get strategy type %s: %v", code, err)
	}
	return id
}

func TestIntegrationRepository_CreateAndGet(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	defer pool.Close()

	repo := NewRepository(pool)
	ctx := context.Background()
	tenantID := getTestTenantID(t)
	strategyTypeID := getStrategyTypeID(t, pool, "PRICE_ARBITRAGE")

	instance := &StrategyInstance{
		ID:             uuid.New(),
		TenantID:       tenantID,
		StrategyTypeID: strategyTypeID,
		Name:           "Test Strategy INT-001",
		Mode:           StrategyModePaper,
		Status:         StrategyStatusDraft,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	err := repo.Create(ctx, instance)
	if err != nil {
		t.Fatalf("failed to create strategy: %v", err)
	}

	// Get by ID
	found, err := repo.GetByID(ctx, instance.ID)
	if err != nil {
		t.Fatalf("failed to get strategy: %v", err)
	}

	if found.Name != instance.Name {
		t.Errorf("expected name %s, got %s", instance.Name, found.Name)
	}
	if found.Mode != instance.Mode {
		t.Errorf("expected mode %s, got %s", instance.Mode, found.Mode)
	}
	if found.Status != instance.Status {
		t.Errorf("expected status %s, got %s", instance.Status, found.Status)
	}
}

func TestIntegrationRepository_List(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	defer pool.Close()

	repo := NewRepository(pool)
	ctx := context.Background()
	tenantID := getTestTenantID(t)
	strategyTypeID := getStrategyTypeID(t, pool, "PRICE_ARBITRAGE")

	// Create multiple strategies
	for i := 0; i < 3; i++ {
		instance := &StrategyInstance{
			ID:             uuid.New(),
			TenantID:       tenantID,
			StrategyTypeID: strategyTypeID,
			Name:           fmt.Sprintf("List Test Strategy %d", i),
			Mode:           StrategyModePaper,
			Status:         StrategyStatusDraft,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}
		if err := repo.Create(ctx, instance); err != nil {
			t.Fatalf("failed to create strategy: %v", err)
		}
	}

	instances, err := repo.List(ctx, tenantID)
	if err != nil {
		t.Fatalf("failed to list strategies: %v", err)
	}

	if len(instances) < 3 {
		t.Errorf("expected at least 3 strategies, got %d", len(instances))
	}
}

func TestIntegrationRepository_Update(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	defer pool.Close()

	repo := NewRepository(pool)
	ctx := context.Background()
	tenantID := getTestTenantID(t)
	strategyTypeID := getStrategyTypeID(t, pool, "PRICE_ARBITRAGE")

	instance := &StrategyInstance{
		ID:             uuid.New(),
		TenantID:       tenantID,
		StrategyTypeID: strategyTypeID,
		Name:           "Update Test Strategy",
		Mode:           StrategyModePaper,
		Status:         StrategyStatusDraft,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	if err := repo.Create(ctx, instance); err != nil {
		t.Fatalf("failed to create strategy: %v", err)
	}

	// Update
	instance.Name = "Updated Strategy"
	instance.Status = StrategyStatusRunning

	if err := repo.Update(ctx, instance); err != nil {
		t.Fatalf("failed to update strategy: %v", err)
	}

	// Verify update
	found, err := repo.GetByID(ctx, instance.ID)
	if err != nil {
		t.Fatalf("failed to get strategy: %v", err)
	}

	if found.Name != "Updated Strategy" {
		t.Errorf("expected name 'Updated Strategy', got %s", found.Name)
	}
	if found.Status != StrategyStatusRunning {
		t.Errorf("expected status RUNNING, got %s", found.Status)
	}
}

func TestIntegrationRepository_Delete(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	defer pool.Close()

	repo := NewRepository(pool)
	ctx := context.Background()
	tenantID := getTestTenantID(t)
	strategyTypeID := getStrategyTypeID(t, pool, "PRICE_ARBITRAGE")

	instance := &StrategyInstance{
		ID:             uuid.New(),
		TenantID:       tenantID,
		StrategyTypeID: strategyTypeID,
		Name:           "Delete Test Strategy",
		Mode:           StrategyModePaper,
		Status:         StrategyStatusDraft,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	if err := repo.Create(ctx, instance); err != nil {
		t.Fatalf("failed to create strategy: %v", err)
	}

	if err := repo.Delete(ctx, instance.ID); err != nil {
		t.Fatalf("failed to delete strategy: %v", err)
	}

	// Verify deletion
	_, err := repo.GetByID(ctx, instance.ID)
	if err == nil {
		t.Error("expected error when getting deleted strategy")
	}
}

func TestIntegrationRepository_CreateDecision(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	defer pool.Close()

	repo := NewRepository(pool)
	ctx := context.Background()
	tenantID := getTestTenantID(t)
	strategyTypeID := getStrategyTypeID(t, pool, "PRICE_ARBITRAGE")

	// Create strategy first
	instance := &StrategyInstance{
		ID:             uuid.New(),
		TenantID:       tenantID,
		StrategyTypeID: strategyTypeID,
		Name:           "Decision Test Strategy",
		Mode:           StrategyModePaper,
		Status:         StrategyStatusRunning,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	if err := repo.Create(ctx, instance); err != nil {
		t.Fatalf("failed to create strategy: %v", err)
	}

	// Create decision (opportunity_id is nullable)
	decision := &StrategyDecision{
		ID:            uuid.New(),
		StrategyID:    instance.ID,
		OpportunityID: uuid.Nil,
		Action:        "ACCEPT",
		Status:        "PAPER",
		ExecutedAt:    time.Now(),
		CreatedAt:     time.Now(),
	}

	if err := repo.CreateDecision(ctx, decision); err != nil {
		t.Fatalf("failed to create decision: %v", err)
	}

	// List decisions
	decisions, err := repo.ListDecisions(ctx, instance.ID, 10)
	if err != nil {
		t.Fatalf("failed to list decisions: %v", err)
	}

	if len(decisions) != 1 {
		t.Errorf("expected 1 decision, got %d", len(decisions))
	}
}

func TestIntegrationService_CreateAndStart(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	defer pool.Close()

	repo := NewRepository(pool)
	oppSvc := opportunity.NewService(nil, opportunity.DefaultScannerConfig())
	svc := NewService(repo, oppSvc)
	ctx := context.Background()
	tenantID := getTestTenantID(t)

	req := &CreateStrategyRequest{
		Name: "Integration Test Strategy",
		Type: StrategyTypePriceArb,
		Mode: StrategyModePaper,
		Config: &StrategyConfig{
			MinNetEdgeBPS: 10,
			MinConfidence: 0.5,
		},
	}

	instance, err := svc.Create(ctx, req, tenantID)
	if err != nil {
		t.Fatalf("failed to create strategy: %v", err)
	}

	if instance.Status != StrategyStatusDraft {
		t.Errorf("expected status DRAFT, got %s", instance.Status)
	}

	// Start strategy
	if err := svc.Start(ctx, instance.ID); err != nil {
		t.Fatalf("failed to start strategy: %v", err)
	}

	// Verify started
	started, err := svc.GetByID(ctx, instance.ID)
	if err != nil {
		t.Fatalf("failed to get strategy: %v", err)
	}

	if started.Status != StrategyStatusRunning {
		t.Errorf("expected status RUNNING, got %s", started.Status)
	}

	// Stop strategy
	if err := svc.Stop(ctx, instance.ID); err != nil {
		t.Fatalf("failed to stop strategy: %v", err)
	}

	// Verify stopped
	stopped, err := svc.GetByID(ctx, instance.ID)
	if err != nil {
		t.Fatalf("failed to get strategy: %v", err)
	}

	if stopped.Status != StrategyStatusPaused {
		t.Errorf("expected status PAUSED, got %s", stopped.Status)
	}
}

func TestIntegrationEngine_StartStop(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	defer pool.Close()

	_ = pool
	oppSvc := opportunity.NewService(nil, opportunity.DefaultScannerConfig())
	engine := NewEngine()
	ctx := context.Background()

	instance := &StrategyInstance{
		ID:        uuid.New(),
		Name:      "Engine Test Strategy",
		Mode:      StrategyModePaper,
		Status:    StrategyStatusRunning,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// Start instance
	engine.StartInstance(ctx, instance, oppSvc)

	running := engine.GetRunningInstances()
	if len(running) != 1 {
		t.Errorf("expected 1 running instance, got %d", len(running))
	}

	// Stop instance
	engine.StopInstance(instance.ID)

	running = engine.GetRunningInstances()
	if len(running) != 0 {
		t.Errorf("expected 0 running instances, got %d", len(running))
	}
}
