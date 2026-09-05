//go:build integration

package venue

import (
	"context"
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

	// Clean up tables before test
	pool.Exec(ctx, "DELETE FROM venue_instruments")
	pool.Exec(ctx, "DELETE FROM instruments")
	pool.Exec(ctx, "DELETE FROM venues")

	return pool
}

func TestVenueRepository_CreateAndGet(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	defer pool.Close()

	repo := NewRepository(pool)
	ctx := context.Background()

	v := &Venue{
		ID:        uuid.New(),
		Code:      "integration-test-venue",
		Name:      "Integration Test Venue",
		VenueType: VenueTypeCEX,
		Status:    VenueStatusActive,
	}

	err := repo.Create(ctx, v)
	if err != nil {
		t.Fatalf("failed to create venue: %v", err)
	}

	// Get by ID
	found, err := repo.GetByID(ctx, v.ID)
	if err != nil {
		t.Fatalf("failed to get venue by ID: %v", err)
	}

	if found.Code != v.Code {
		t.Errorf("expected code %s, got %s", v.Code, found.Code)
	}
	if found.Name != v.Name {
		t.Errorf("expected name %s, got %s", v.Name, found.Name)
	}

	// Get by code
	foundByCode, err := repo.GetByCode(ctx, v.Code)
	if err != nil {
		t.Fatalf("failed to get venue by code: %v", err)
	}

	if foundByCode.ID != v.ID {
		t.Errorf("expected ID %s, got %s", v.ID, foundByCode.ID)
	}

	// Clean up
	repo.Delete(ctx, v.ID)
}

func TestVenueRepository_UniqueCode(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	defer pool.Close()

	repo := NewRepository(pool)
	ctx := context.Background()

	v1 := &Venue{
		ID:        uuid.New(),
		Code:      "unique-code-test",
		Name:      "Venue 1",
		VenueType: VenueTypeCEX,
		Status:    VenueStatusActive,
	}

	v2 := &Venue{
		ID:        uuid.New(),
		Code:      "unique-code-test", // Same code
		Name:      "Venue 2",
		VenueType: VenueTypeCEX,
		Status:    VenueStatusActive,
	}

	err := repo.Create(ctx, v1)
	if err != nil {
		t.Fatalf("failed to create first venue: %v", err)
	}

	err = repo.Create(ctx, v2)
	if err == nil {
		t.Error("expected error for duplicate code, got nil")
	}

	// Clean up
	repo.Delete(ctx, v1.ID)
}

func TestVenueRepository_List(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	defer pool.Close()

	repo := NewRepository(pool)
	ctx := context.Background()

	v1 := &Venue{ID: uuid.New(), Code: "list-test-1", Name: "Venue 1", VenueType: VenueTypeCEX, Status: VenueStatusActive}
	v2 := &Venue{ID: uuid.New(), Code: "list-test-2", Name: "Venue 2", VenueType: VenueTypeCEX, Status: VenueStatusActive}

	repo.Create(ctx, v1)
	repo.Create(ctx, v2)

	venues, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("failed to list venues: %v", err)
	}

	if len(venues) < 2 {
		t.Errorf("expected at least 2 venues, got %d", len(venues))
	}

	// Clean up
	repo.Delete(ctx, v1.ID)
	repo.Delete(ctx, v2.ID)
}

func TestVenueRepository_Update(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	defer pool.Close()

	repo := NewRepository(pool)
	ctx := context.Background()

	v := &Venue{
		ID:        uuid.New(),
		Code:      "update-test",
		Name:      "Original Name",
		VenueType: VenueTypeCEX,
		Status:    VenueStatusActive,
	}

	repo.Create(ctx, v)

	v.Name = "Updated Name"
	err := repo.Update(ctx, v)
	if err != nil {
		t.Fatalf("failed to update venue: %v", err)
	}

	found, _ := repo.GetByID(ctx, v.ID)
	if found.Name != "Updated Name" {
		t.Errorf("expected name 'Updated Name', got '%s'", found.Name)
	}

	// Clean up
	repo.Delete(ctx, v.ID)
}

func TestVenueRepository_Delete(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	defer pool.Close()

	repo := NewRepository(pool)
	ctx := context.Background()

	v := &Venue{
		ID:        uuid.New(),
		Code:      "delete-test",
		Name:      "To Delete",
		VenueType: VenueTypeCEX,
		Status:    VenueStatusActive,
	}

	repo.Create(ctx, v)

	err := repo.Delete(ctx, v.ID)
	if err != nil {
		t.Fatalf("failed to delete venue: %v", err)
	}

	_, err = repo.GetByID(ctx, v.ID)
	if err == nil {
		t.Error("expected error after deletion, got nil")
	}
}
