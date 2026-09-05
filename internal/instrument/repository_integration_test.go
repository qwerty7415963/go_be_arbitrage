//go:build integration

package instrument

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

	return pool
}

func TestInstrumentRepository_CreateAndGet(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	defer pool.Close()

	repo := NewRepository(pool)
	ctx := context.Background()

	inst := &Instrument{
		ID:              uuid.New(),
		CanonicalSymbol: "BTC-USDT-INT-TEST",
		BaseAsset:       "BTC",
		QuoteAsset:      "USDT",
		InstrumentType:  InstrumentTypePerp,
		ContractType:    ContractTypeLinear,
		PriceTick:       "0.1",
		QuantityStep:    "0.001",
		DiscoveryStatus: DiscoveryStatusDiscovered,
		TradingEnabled:  false,
	}

	err := repo.Create(ctx, inst)
	if err != nil {
		t.Fatalf("failed to create instrument: %v", err)
	}

	// Get by ID
	found, err := repo.GetByID(ctx, inst.ID)
	if err != nil {
		t.Fatalf("failed to get instrument by ID: %v", err)
	}

	if found.CanonicalSymbol != inst.CanonicalSymbol {
		t.Errorf("expected canonical symbol %s, got %s", inst.CanonicalSymbol, found.CanonicalSymbol)
	}

	// Get by canonical symbol
	foundBySymbol, err := repo.GetByCanonicalSymbol(ctx, inst.CanonicalSymbol)
	if err != nil {
		t.Fatalf("failed to get instrument by canonical symbol: %v", err)
	}

	if foundBySymbol.ID != inst.ID {
		t.Errorf("expected ID %s, got %s", inst.ID, foundBySymbol.ID)
	}

	// Clean up
	repo.Delete(ctx, inst.ID)
}

func TestInstrumentRepository_UniqueCanonicalSymbol(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	defer pool.Close()

	repo := NewRepository(pool)
	ctx := context.Background()

	inst1 := &Instrument{
		ID:              uuid.New(),
		CanonicalSymbol: "ETH-USDT-UNIQUE",
		BaseAsset:       "ETH",
		QuoteAsset:      "USDT",
		InstrumentType:  InstrumentTypePerp,
		PriceTick:       "0.01",
		QuantityStep:    "0.01",
		DiscoveryStatus: DiscoveryStatusDiscovered,
		TradingEnabled:  false,
	}

	inst2 := &Instrument{
		ID:              uuid.New(),
		CanonicalSymbol: "ETH-USDT-UNIQUE", // Same symbol
		BaseAsset:       "ETH",
		QuoteAsset:      "USDT",
		InstrumentType:  InstrumentTypePerp,
		PriceTick:       "0.01",
		QuantityStep:    "0.01",
		DiscoveryStatus: DiscoveryStatusDiscovered,
		TradingEnabled:  false,
	}

	err := repo.Create(ctx, inst1)
	if err != nil {
		t.Fatalf("failed to create first instrument: %v", err)
	}

	err = repo.Create(ctx, inst2)
	if err == nil {
		t.Error("expected error for duplicate canonical symbol, got nil")
	}

	// Clean up
	repo.Delete(ctx, inst1.ID)
}

func TestInstrumentRepository_ListTradable(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	defer pool.Close()

	repo := NewRepository(pool)
	ctx := context.Background()

	// Only this one should be tradable
	inst1 := &Instrument{
		ID:              uuid.New(),
		CanonicalSymbol: "BTC-USDT-TRADABLE",
		BaseAsset:       "BTC",
		QuoteAsset:      "USDT",
		InstrumentType:  InstrumentTypePerp,
		PriceTick:       "0.1",
		QuantityStep:    "0.001",
		DiscoveryStatus: DiscoveryStatusReviewed,
		TradingEnabled:  true,
	}

	inst2 := &Instrument{
		ID:              uuid.New(),
		CanonicalSymbol: "ETH-USDT-NOT-TRADABLE",
		BaseAsset:       "ETH",
		QuoteAsset:      "USDT",
		InstrumentType:  InstrumentTypePerp,
		PriceTick:       "0.01",
		QuantityStep:    "0.01",
		DiscoveryStatus: DiscoveryStatusReviewed,
		TradingEnabled:  false, // Not tradable
	}

	repo.Create(ctx, inst1)
	repo.Create(ctx, inst2)

	tradable, err := repo.ListTradable(ctx)
	if err != nil {
		t.Fatalf("failed to list tradable: %v", err)
	}

	// Check that inst1 is in the list
	found := false
	for _, inst := range tradable {
		if inst.ID == inst1.ID {
			found = true
			break
		}
	}

	if !found {
		t.Error("expected tradable instrument to be in list")
	}

	// Clean up
	repo.Delete(ctx, inst1.ID)
	repo.Delete(ctx, inst2.ID)
}

func TestInstrumentRepository_EnableTrading(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	defer pool.Close()

	repo := NewRepository(pool)
	ctx := context.Background()

	inst := &Instrument{
		ID:              uuid.New(),
		CanonicalSymbol: "SOL-USDT-ENABLE",
		BaseAsset:       "SOL",
		QuoteAsset:      "USDT",
		InstrumentType:  InstrumentTypePerp,
		PriceTick:       "0.01",
		QuantityStep:    "0.01",
		DiscoveryStatus: DiscoveryStatusReviewed,
		TradingEnabled:  false,
	}

	repo.Create(ctx, inst)

	// Enable trading
	err := repo.EnableTrading(ctx, inst.ID, true)
	if err != nil {
		t.Fatalf("failed to enable trading: %v", err)
	}

	// Verify
	found, _ := repo.GetByID(ctx, inst.ID)
	if !found.TradingEnabled {
		t.Error("expected trading_enabled to be true")
	}

	// Disable trading
	repo.EnableTrading(ctx, inst.ID, false)

	found, _ = repo.GetByID(ctx, inst.ID)
	if found.TradingEnabled {
		t.Error("expected trading_enabled to be false")
	}

	// Clean up
	repo.Delete(ctx, inst.ID)
}

func TestVenueInstrumentRepository_CreateAndQuery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	defer pool.Close()

	repo := NewRepository(pool)
	ctx := context.Background()

	// First create a venue and instrument
	venueRepo := NewRepository(pool)
	v := &Venue{
		ID:        uuid.New(),
		Code:      "venue-for-mapping",
		Name:      "Venue for Mapping",
		VenueType: VenueTypeCEX,
		Status:    VenueStatusActive,
	}
	venueRepo.Create(ctx, v)

	inst := &Instrument{
		ID:              uuid.New(),
		CanonicalSymbol: "BTC-USDT-MAPPING",
		BaseAsset:       "BTC",
		QuoteAsset:      "USDT",
		InstrumentType:  InstrumentTypePerp,
		PriceTick:       "0.1",
		QuantityStep:    "0.001",
		DiscoveryStatus: DiscoveryStatusDiscovered,
		TradingEnabled:  false,
	}
	repo.Create(ctx, inst)

	// Create venue instrument mapping
	vi := &VenueInstrument{
		ID:            uuid.New(),
		VenueID:       v.ID,
		InstrumentID:  inst.ID,
		VenueSymbol:   "BTCUSDT",
		Status:        "ACTIVE",
		VenueMetadata: map[string]interface{}{},
	}

	err := repo.CreateVenueInstrument(ctx, vi)
	if err != nil {
		t.Fatalf("failed to create venue instrument: %v", err)
	}

	// Query
	found, err := repo.GetVenueInstrument(ctx, v.ID, "BTCUSDT")
	if err != nil {
		t.Fatalf("failed to get venue instrument: %v", err)
	}

	if found.VenueID != v.ID {
		t.Errorf("expected venue ID %s, got %s", v.ID, found.VenueID)
	}

	// List by venue
	list, err := repo.ListVenueInstruments(ctx, v.ID)
	if err != nil {
		t.Fatalf("failed to list venue instruments: %v", err)
	}

	if len(list) < 1 {
		t.Errorf("expected at least 1 venue instrument, got %d", len(list))
	}

	// Clean up
	pool.Exec(ctx, "DELETE FROM venue_instruments WHERE id = $1", vi.ID)
	repo.Delete(ctx, inst.ID)
	venueRepo.Delete(ctx, v.ID)
}

func TestVenueInstrumentRepository_UniqueVenueSymbol(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	defer pool.Close()

	repo := NewRepository(pool)
	ctx := context.Background()

	// Create venue
	v := &Venue{
		ID:        uuid.New(),
		Code:      "unique-mapping-venue",
		Name:      "Unique Mapping Venue",
		VenueType: VenueTypeCEX,
		Status:    VenueStatusActive,
	}
	repo.Create(ctx, v)

	// Create two instruments
	inst1 := &Instrument{
		ID:              uuid.New(),
		CanonicalSymbol: "BTC-USDT-UNIQUE-MAP-1",
		BaseAsset:       "BTC",
		QuoteAsset:      "USDT",
		InstrumentType:  InstrumentTypePerp,
		PriceTick:       "0.1",
		QuantityStep:    "0.001",
		DiscoveryStatus: DiscoveryStatusDiscovered,
		TradingEnabled:  false,
	}
	inst2 := &Instrument{
		ID:              uuid.New(),
		CanonicalSymbol: "ETH-USDT-UNIQUE-MAP-2",
		BaseAsset:       "ETH",
		QuoteAsset:      "USDT",
		InstrumentType:  InstrumentTypePerp,
		PriceTick:       "0.01",
		QuantityStep:    "0.01",
		DiscoveryStatus: DiscoveryStatusDiscovered,
		TradingEnabled:  false,
	}

	repo.Create(ctx, inst1)
	repo.Create(ctx, inst2)

	// Create first mapping
	vi1 := &VenueInstrument{
		ID:            uuid.New(),
		VenueID:       v.ID,
		InstrumentID:  inst1.ID,
		VenueSymbol:   "BTCUSDT",
		Status:        "ACTIVE",
		VenueMetadata: map[string]interface{}{},
	}

	err := repo.CreateVenueInstrument(ctx, vi1)
	if err != nil {
		t.Fatalf("failed to create first venue instrument: %v", err)
	}

	// Create second mapping with same venue symbol - should fail
	vi2 := &VenueInstrument{
		ID:            uuid.New(),
		VenueID:       v.ID,
		InstrumentID:  inst2.ID,
		VenueSymbol:   "BTCUSDT", // Same symbol
		Status:        "ACTIVE",
		VenueMetadata: map[string]interface{}{},
	}

	err = repo.CreateVenueInstrument(ctx, vi2)
	if err == nil {
		t.Error("expected error for duplicate venue symbol, got nil")
	}

	// Clean up
	pool.Exec(ctx, "DELETE FROM venue_instruments WHERE id = $1", vi1.ID)
	repo.Delete(ctx, inst1.ID)
	repo.Delete(ctx, inst2.ID)
	repo.Delete(ctx, v.ID)
}
