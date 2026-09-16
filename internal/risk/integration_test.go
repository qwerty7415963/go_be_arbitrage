//go:build integration

package risk

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
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

	// Clean up
	pool.Exec(ctx, "DELETE FROM risk_checks")
	pool.Exec(ctx, "DELETE FROM risk_policies")

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

	t.Setenv("TEST_TENANT_ID", tenantID.String())
	return pool
}

func getTestTenantID(t *testing.T) uuid.UUID {
	t.Helper()
	tenantIDStr := os.Getenv("TEST_TENANT_ID")
	if tenantIDStr == "" {
		t.Fatal("TEST_TENANT_ID not set")
	}
	id, _ := uuid.Parse(tenantIDStr)
	return id
}

func TestIntegrationRepository_CreatePolicy(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	defer pool.Close()

	repo := NewRepository(pool)
	ctx := context.Background()
	tenantID := getTestTenantID(t)

	policy := &RiskPolicy{
		ID:         uuid.New(),
		TenantID:   tenantID,
		Name:       "Test Risk Policy",
		PolicyType: PolicyTypeGlobal,
		Config:     DefaultRiskConfig(),
		Status:     PolicyStatusActive,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	if err := repo.CreatePolicy(ctx, policy); err != nil {
		t.Fatalf("failed to create policy: %v", err)
	}

	found, err := repo.GetPolicyByID(ctx, policy.ID)
	if err != nil {
		t.Fatalf("failed to get policy: %v", err)
	}

	if found.Name != policy.Name {
		t.Errorf("expected name %s, got %s", policy.Name, found.Name)
	}
}

func TestIntegrationRepository_ListPolicies(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	defer pool.Close()

	repo := NewRepository(pool)
	ctx := context.Background()
	tenantID := getTestTenantID(t)

	for i := 0; i < 3; i++ {
		policy := &RiskPolicy{
			ID:         uuid.New(),
			TenantID:   tenantID,
			Name:       fmt.Sprintf("List Policy %d", i),
			PolicyType: PolicyTypeGlobal,
			Config:     DefaultRiskConfig(),
			Status:     PolicyStatusActive,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		}
		if err := repo.CreatePolicy(ctx, policy); err != nil {
			t.Fatalf("failed to create policy: %v", err)
		}
	}

	policies, err := repo.ListPolicies(ctx, tenantID)
	if err != nil {
		t.Fatalf("failed to list policies: %v", err)
	}

	if len(policies) < 3 {
		t.Errorf("expected at least 3 policies, got %d", len(policies))
	}
}

func TestIntegrationRepository_UpdatePolicy(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	defer pool.Close()

	repo := NewRepository(pool)
	ctx := context.Background()
	tenantID := getTestTenantID(t)

	policy := &RiskPolicy{
		ID:         uuid.New(),
		TenantID:   tenantID,
		Name:       "Update Policy",
		PolicyType: PolicyTypeGlobal,
		Config:     DefaultRiskConfig(),
		Status:     PolicyStatusActive,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	if err := repo.CreatePolicy(ctx, policy); err != nil {
		t.Fatalf("failed to create policy: %v", err)
	}

	policy.Name = "Updated Policy"
	if err := repo.UpdatePolicy(ctx, policy); err != nil {
		t.Fatalf("failed to update policy: %v", err)
	}

	found, err := repo.GetPolicyByID(ctx, policy.ID)
	if err != nil {
		t.Fatalf("failed to get policy: %v", err)
	}

	if found.Name != "Updated Policy" {
		t.Errorf("expected name 'Updated Policy', got %s", found.Name)
	}
}

func TestIntegrationRepository_DeletePolicy(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	defer pool.Close()

	repo := NewRepository(pool)
	ctx := context.Background()
	tenantID := getTestTenantID(t)

	policy := &RiskPolicy{
		ID:         uuid.New(),
		TenantID:   tenantID,
		Name:       "Delete Policy",
		PolicyType: PolicyTypeGlobal,
		Config:     DefaultRiskConfig(),
		Status:     PolicyStatusActive,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	if err := repo.CreatePolicy(ctx, policy); err != nil {
		t.Fatalf("failed to create policy: %v", err)
	}

	if err := repo.DeletePolicy(ctx, policy.ID); err != nil {
		t.Fatalf("failed to delete policy: %v", err)
	}

	_, err := repo.GetPolicyByID(ctx, policy.ID)
	if err == nil {
		t.Error("expected error when getting deleted policy")
	}
}

func TestIntegrationRepository_CreateCheck(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	defer pool.Close()

	repo := NewRepository(pool)
	ctx := context.Background()

	check := &RiskCheck{
		ID:        uuid.New(),
		CheckType: "BALANCE",
		Result:    CheckResultPass,
		ObservedValue: map[string]interface{}{
			"balance": 10000,
		},
		CreatedAt: time.Now(),
	}

	if err := repo.CreateCheck(ctx, check); err != nil {
		t.Fatalf("failed to create check: %v", err)
	}
}

func TestIntegrationService_CreateAndListPolicies(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	defer pool.Close()

	repo := NewRepository(pool)
	svc := NewService(repo, DefaultRiskConfig())
	ctx := context.Background()
	tenantID := getTestTenantID(t)

	req := &CreatePolicyRequest{
		Name:       "Service Policy",
		PolicyType: PolicyTypeGlobal,
		Config:     DefaultRiskConfig(),
	}

	policy, err := svc.CreatePolicy(ctx, req, tenantID)
	if err != nil {
		t.Fatalf("failed to create policy: %v", err)
	}

	policies, err := svc.ListPolicies(ctx, tenantID)
	if err != nil {
		t.Fatalf("failed to list policies: %v", err)
	}

	if len(policies) < 1 {
		t.Error("expected at least 1 policy")
	}

	if policies[0].ID != policy.ID {
		t.Error("expected policy ID to match")
	}
}

func TestIntegrationService_KillSwitch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	defer pool.Close()

	repo := NewRepository(pool)
	svc := NewService(repo, DefaultRiskConfig())

	if svc.IsKillSwitchActive() {
		t.Error("expected kill switch inactive initially")
	}

	svc.EnableKillSwitch("test")
	if !svc.IsKillSwitchActive() {
		t.Error("expected kill switch active")
	}

	ks := svc.GetKillSwitch()
	if ks.Reason != "test" {
		t.Errorf("expected reason 'test', got %s", ks.Reason)
	}

	svc.DisableKillSwitch()
	if svc.IsKillSwitchActive() {
		t.Error("expected kill switch inactive after disable")
	}
}
