package database

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/qwerty7415963/go_be_arbitrage/internal/config"
)

func TestNew_InvalidConfig(t *testing.T) {
	ctx := context.Background()
	cfg := config.DatabaseConfig{
		Host:     "invalid-host",
		Port:     5432,
		User:     "test",
		Password: "test",
		Name:     "test",
		SSLMode:  "disable",
	}

	_, err := New(ctx, cfg)
	if err == nil {
		t.Error("expected error for invalid config")
	}
}

func TestDSN(t *testing.T) {
	cfg := config.Config{
		Database: config.DatabaseConfig{
			Host:     "localhost",
			Port:     5432,
			User:     "postgres",
			Password: "secret",
			Name:     "testdb",
			SSLMode:  "disable",
		},
	}

	expected := "host=localhost port=5432 user=postgres password=secret dbname=testdb sslmode=disable"
	if cfg.DSN() != expected {
		t.Errorf("DSN() = %s, want %s", cfg.DSN(), expected)
	}
}

func TestIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	dbHost := os.Getenv("ARBITRAGE_DB_HOST")
	if dbHost == "" {
		dbHost = "localhost"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cfg := config.DatabaseConfig{
		Host:            dbHost,
		Port:            5432,
		User:            "test",
		Password:        "test",
		Name:            "arbitrage_test",
		SSLMode:         "disable",
		MaxOpenConns:    5,
		MaxIdleConns:    5,
		ConnMaxLifetime: 5 * time.Minute,
	}

	db, err := New(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to connect to database: %v", err)
	}
	defer db.Close()

	if err := db.Ping(ctx); err != nil {
		t.Fatalf("failed to ping database: %v", err)
	}

	stats := db.Stats()
	if stats == nil {
		t.Error("expected stats to be non-nil")
	}

	fmt.Printf("Database connected: total=%d, idle=%d, acquired=%d\n",
		stats.TotalConns, stats.IdleConns, stats.AcquiredConns)
}
