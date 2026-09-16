//go:build integration

package execution

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func setupTestDB(t *testing.T) (*pgxpool.Pool, uuid.UUID, uuid.UUID) {
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

	// Clean up only child tables
	pool.Exec(ctx, "DELETE FROM fills")
	pool.Exec(ctx, "DELETE FROM orders")
	pool.Exec(ctx, "DELETE FROM execution_legs")
	pool.Exec(ctx, "DELETE FROM executions")

	// Ensure system tenant
	var tenantID uuid.UUID
	err = pool.QueryRow(ctx, "SELECT id FROM tenants LIMIT 1").Scan(&tenantID)
	if err != nil {
		tenantID = uuid.New()
		pool.Exec(ctx, "INSERT INTO tenants (id, name, status) VALUES ($1, 'system', 'ACTIVE')", tenantID)
	}

	// Ensure strategy type
	var strategyTypeID uuid.UUID
	err = pool.QueryRow(ctx, "SELECT id FROM strategy_types WHERE code = 'PRICE_ARBITRAGE'").Scan(&strategyTypeID)
	if err != nil {
		strategyTypeID = uuid.New()
		pool.Exec(ctx, "INSERT INTO strategy_types (id, code, name, status) VALUES ($1, 'PRICE_ARBITRAGE', 'Price Arbitrage', 'ACTIVE')", strategyTypeID)
	}

	// Create strategy instance (use unique name to avoid UNIQUE constraint)
	strategyInstanceID := uuid.New()
	strategyName := fmt.Sprintf("test-strategy-%s", uuid.New().String()[:8])
	_, err = pool.Exec(ctx, "INSERT INTO strategy_instances (id, tenant_id, strategy_type_id, name, mode, status) VALUES ($1, $2, $3, $4, 'PAPER', 'DRAFT')",
		strategyInstanceID, tenantID, strategyTypeID, strategyName)
	if err != nil {
		t.Fatalf("failed to create strategy instance: %v", err)
	}

	// Create venue (use unique code)
	venueID := uuid.New()
	venueCode := fmt.Sprintf("test-venue-%s", uuid.New().String()[:8])
	_, err = pool.Exec(ctx, "INSERT INTO venues (id, code, name, venue_type, status) VALUES ($1, $2, 'Test Venue', 'CEX', 'ACTIVE')",
		venueID, venueCode)
	if err != nil {
		t.Fatalf("failed to create venue: %v", err)
	}

	// Create venue account
	venueAccountID := uuid.New()
	_, err = pool.Exec(ctx, "INSERT INTO venue_accounts (id, tenant_id, venue_id, account_name, status) VALUES ($1, $2, $3, 'test-account', 'ACTIVE')",
		venueAccountID, tenantID, venueID)
	if err != nil {
		t.Fatalf("failed to create venue account: %v", err)
	}

	// Create instrument (use unique symbol)
	instrumentID := uuid.New()
	instrumentSymbol := fmt.Sprintf("BTC-USDT-%s", uuid.New().String()[:8])
	_, err = pool.Exec(ctx, "INSERT INTO instruments (id, canonical_symbol, base_asset, quote_asset, instrument_type, contract_type, discovery_status, trading_enabled, price_tick, quantity_step) VALUES ($1, $2, 'BTC', 'USDT', 'PERP', 'LINEAR', 'REVIEWED', true, '0.1', '0.001')",
		instrumentID, instrumentSymbol)
	if err != nil {
		t.Fatalf("failed to create instrument: %v", err)
	}

	t.Setenv("TEST_TENANT_ID", tenantID.String())
	t.Setenv("TEST_STRATEGY_ID", strategyInstanceID.String())
	t.Setenv("TEST_VENUE_ACCOUNT_ID", venueAccountID.String())
	t.Setenv("TEST_INSTRUMENT_ID", instrumentID.String())

	return pool, tenantID, strategyInstanceID
}

func TestIntegrationRepository_CreateExecution(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool, tenantID, strategyInstanceID := setupTestDB(t)
	defer pool.Close()

	repo := NewRepository(pool)
	ctx := context.Background()

	exec := &Execution{
		ID:                 uuid.New(),
		TenantID:           tenantID,
		StrategyInstanceID: strategyInstanceID,
		ExecutionMode:      ExecutionModePaper,
		Status:             ExecutionStatusCreated,
		IntentJSON:         map[string]interface{}{"test": true},
		CreatedAt:          time.Now(),
	}

	if err := repo.CreateExecution(ctx, exec); err != nil {
		t.Fatalf("failed to create execution: %v", err)
	}

	found, err := repo.GetExecutionByID(ctx, exec.ID)
	if err != nil {
		t.Fatalf("failed to get execution: %v", err)
	}

	if found.Status != ExecutionStatusCreated {
		t.Errorf("expected status CREATED, got %s", found.Status)
	}
}

func TestIntegrationRepository_CreateLeg(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool, tenantID, strategyInstanceID := setupTestDB(t)
	defer pool.Close()

	repo := NewRepository(pool)
	ctx := context.Background()

	exec := &Execution{
		ID:                 uuid.New(),
		TenantID:           tenantID,
		StrategyInstanceID: strategyInstanceID,
		ExecutionMode:      ExecutionModePaper,
		Status:             ExecutionStatusCreated,
		IntentJSON:         map[string]interface{}{},
		CreatedAt:          time.Now(),
	}
	if err := repo.CreateExecution(ctx, exec); err != nil {
		t.Fatalf("failed to create execution: %v", err)
	}

	venueAccountID, _ := uuid.Parse(os.Getenv("TEST_VENUE_ACCOUNT_ID"))
	instrumentID, _ := uuid.Parse(os.Getenv("TEST_INSTRUMENT_ID"))

	leg := &ExecutionLeg{
		ID:             uuid.New(),
		ExecutionID:    exec.ID,
		LegIndex:       0,
		LegRole:        "PRIMARY",
		VenueAccountID: venueAccountID,
		InstrumentID:   instrumentID,
		TargetSide:     SideBuy,
		Status:         LegStatusPending,
		Metadata:       map[string]interface{}{},
	}

	if err := repo.CreateLeg(ctx, leg); err != nil {
		t.Fatalf("failed to create leg: %v", err)
	}

	legs, err := repo.ListLegsByExecution(ctx, exec.ID)
	if err != nil {
		t.Fatalf("failed to list legs: %v", err)
	}

	if len(legs) != 1 {
		t.Errorf("expected 1 leg, got %d", len(legs))
	}
}

func TestIntegrationRepository_CreateOrder(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool, _, _ := setupTestDB(t)
	defer pool.Close()

	repo := NewRepository(pool)
	ctx := context.Background()

	venueAccountID, _ := uuid.Parse(os.Getenv("TEST_VENUE_ACCOUNT_ID"))
	instrumentID, _ := uuid.Parse(os.Getenv("TEST_INSTRUMENT_ID"))

	order := &Order{
		ID:              uuid.New(),
		VenueAccountID:  venueAccountID,
		InstrumentID:    instrumentID,
		ClientOrderID:   fmt.Sprintf("test-order-%s", uuid.New().String()[:8]),
		Side:            SideBuy,
		OrderType:       "LIMIT",
		Status:          OrderStatusCreated,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}

	if err := repo.CreateOrder(ctx, order); err != nil {
		t.Fatalf("failed to create order: %v", err)
	}

	found, err := repo.GetOrderByClientOrderID(ctx, order.VenueAccountID, order.ClientOrderID)
	if err != nil {
		t.Fatalf("failed to get order: %v", err)
	}

	if found.Status != OrderStatusCreated {
		t.Errorf("expected status CREATED, got %s", found.Status)
	}
}

func TestIntegrationRepository_CreateFill(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool, _, _ := setupTestDB(t)
	defer pool.Close()

	repo := NewRepository(pool)
	ctx := context.Background()

	venueAccountID, _ := uuid.Parse(os.Getenv("TEST_VENUE_ACCOUNT_ID"))
	instrumentID, _ := uuid.Parse(os.Getenv("TEST_INSTRUMENT_ID"))

	order := &Order{
		ID:              uuid.New(),
		VenueAccountID:  venueAccountID,
		InstrumentID:    instrumentID,
		ClientOrderID:   fmt.Sprintf("fill-order-%s", uuid.New().String()[:8]),
		Side:            SideBuy,
		OrderType:       "LIMIT",
		Status:          OrderStatusFilled,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
	if err := repo.CreateOrder(ctx, order); err != nil {
		t.Fatalf("failed to create order: %v", err)
	}

	fill := &Fill{
		ID:             uuid.New(),
		OrderID:        order.ID,
		VenueAccountID: order.VenueAccountID,
		FilledAt:       time.Now(),
		Quantity:       0.1,
		Price:          50000,
	}

	if err := repo.CreateFill(ctx, fill); err != nil {
		t.Fatalf("failed to create fill: %v", err)
	}

	fills, err := repo.ListFillsByOrder(ctx, order.ID)
	if err != nil {
		t.Fatalf("failed to list fills: %v", err)
	}

	if len(fills) != 1 {
		t.Errorf("expected 1 fill, got %d", len(fills))
	}
}

func TestIntegrationRepository_UpdateOrderStatus(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool, _, _ := setupTestDB(t)
	defer pool.Close()

	repo := NewRepository(pool)
	ctx := context.Background()

	venueAccountID, _ := uuid.Parse(os.Getenv("TEST_VENUE_ACCOUNT_ID"))
	instrumentID, _ := uuid.Parse(os.Getenv("TEST_INSTRUMENT_ID"))

	order := &Order{
		ID:              uuid.New(),
		VenueAccountID:  venueAccountID,
		InstrumentID:    instrumentID,
		ClientOrderID:   fmt.Sprintf("update-order-%s", uuid.New().String()[:8]),
		Side:            SideBuy,
		OrderType:       "LIMIT",
		Status:          OrderStatusCreated,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
	if err := repo.CreateOrder(ctx, order); err != nil {
		t.Fatalf("failed to create order: %v", err)
	}

	if err := repo.UpdateOrderStatus(ctx, order.ID, OrderStatusFilled); err != nil {
		t.Fatalf("failed to update order status: %v", err)
	}

	found, err := repo.GetOrderByClientOrderID(ctx, order.VenueAccountID, order.ClientOrderID)
	if err != nil {
		t.Fatalf("failed to get order: %v", err)
	}

	if found.Status != OrderStatusFilled {
		t.Errorf("expected status FILLED, got %s", found.Status)
	}
}
