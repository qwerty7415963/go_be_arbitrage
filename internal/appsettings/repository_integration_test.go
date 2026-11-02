//go:build integration

package appsettings

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func setupAppSettingsDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	host := os.Getenv("ARBITRAGE_DB_HOST")
	if host == "" {
		host = "localhost"
	}
	port := os.Getenv("ARBITRAGE_DB_PORT")
	if port == "" {
		port = "5433"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, "postgres://test:test@"+host+":"+port+"/arbitrage_test?sslmode=disable")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("ping: %v", err)
	}
	pool.Exec(ctx, "DELETE FROM app_settings")
	return pool
}

func TestRepo_GetSet(t *testing.T) {
	pool := setupAppSettingsDB(t)
	defer pool.Close()
	repo := NewRepository(pool)
	ctx := context.Background()
	if err := repo.Set(ctx, SettingKeyTelegramMaxWallets, "100", nil); err != nil {
		t.Fatalf("set: %v", err)
	}
	v, err := repo.Get(ctx, SettingKeyTelegramMaxWallets)
	if err != nil || v != "100" {
		t.Fatalf("get: %s %v", v, err)
	}
	all, err := repo.GetAll(ctx)
	if err != nil || len(all) == 0 {
		t.Fatalf("getall: %v %d", err, len(all))
	}
}

func TestRepo_SeedIfNotExists(t *testing.T) {
	pool := setupAppSettingsDB(t)
	defer pool.Close()
	repo := NewRepository(pool)
	ctx := context.Background()
	if err := repo.SeedIfNotExists(ctx, "foo", "bar"); err != nil {
		t.Fatal(err)
	}
	if err := repo.SeedIfNotExists(ctx, "foo", "baz"); err != nil {
		t.Fatal(err)
	}
	v, _ := repo.Get(ctx, "foo")
	if v != "bar" {
		t.Fatalf("expected bar, got %s", v)
	}
}
