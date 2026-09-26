//go:build integration

package database

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qwerty7415963/go_be_arbitrage/migrations"
)

const migrateTestDBName = "arbitrage_migrate_test"

func migrateEnv(t *testing.T) (host, port string) {
	t.Helper()
	host = os.Getenv("ARBITRAGE_DB_HOST")
	if host == "" {
		host = "localhost"
	}
	port = os.Getenv("ARBITRAGE_DB_PORT")
	if port == "" {
		port = "5433"
	}
	return host, port
}

// setupMigrateTestDB creates a clean, dedicated database so migration tests
// never touch the shared arbitrage_test schema. Returns its postgres:// URL.
func setupMigrateTestDB(t *testing.T) string {
	t.Helper()
	host, port := migrateEnv(t)

	adminDSN := fmt.Sprintf("postgres://test:test@%s:%s/postgres?sslmode=disable", host, port)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	admin, err := pgxpool.New(ctx, adminDSN)
	if err != nil {
		t.Fatalf("connect to maintenance db: %v", err)
	}
	defer admin.Close()
	if err := admin.Ping(ctx); err != nil {
		t.Fatalf("ping maintenance db (is docker-up running?): %v", err)
	}

	if _, err := admin.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", migrateTestDBName)); err != nil {
		t.Fatalf("drop test database: %v", err)
	}
	if _, err := admin.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", migrateTestDBName)); err != nil {
		t.Fatalf("create test database: %v", err)
	}

	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer ccancel()
		admin.Exec(cctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", migrateTestDBName))
	})

	return fmt.Sprintf("postgres://test:test@%s:%s/%s?sslmode=disable", host, port, migrateTestDBName)
}

// expectedVersion counts the embedded *.up.sql files so the test stays
// correct when new migrations are added.
func expectedVersion(t *testing.T) uint {
	t.Helper()
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}
	var n uint
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".up.sql") {
			n++
		}
	}
	if n == 0 {
		t.Fatal("no embedded migrations found")
	}
	return n
}

// MIG-01 + MIG-05: up applies all migrations on a clean database
func TestMigrate_Up(t *testing.T) {
	url := setupMigrateTestDB(t)

	msg, err := RunMigrate(url, []string{"up"})
	if err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	if msg != "migrations applied" {
		t.Errorf("unexpected message: %q", msg)
	}

	// Verify schema actually exists
	host, port := migrateEnv(t)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, fmt.Sprintf("postgres://test:test@%s:%s/%s?sslmode=disable", host, port, migrateTestDBName))
	if err != nil {
		t.Fatalf("connect to migrated db: %v", err)
	}
	defer pool.Close()

	for _, table := range []string{"users", "venues", "instruments", "fills", "wallet_addresses"} {
		var exists bool
		if err := pool.QueryRow(ctx,
			"SELECT to_regclass($1) IS NOT NULL", table).Scan(&exists); err != nil {
			t.Fatalf("check table %s: %v", table, err)
		}
		if !exists {
			t.Errorf("table %s missing after up", table)
		}
	}
}

// MIG-04: up on an up-to-date database is a no-op
func TestMigrate_UpIdempotent(t *testing.T) {
	url := setupMigrateTestDB(t)

	if _, err := RunMigrate(url, []string{"up"}); err != nil {
		t.Fatalf("first migrate up: %v", err)
	}
	msg, err := RunMigrate(url, []string{"up"})
	if err != nil {
		t.Fatalf("second migrate up: %v", err)
	}
	if msg != "migrations up to date" {
		t.Errorf("expected no-op message, got %q", msg)
	}
}

// MIG-03: version reports empty DB and the latest version after up
func TestMigrate_Version(t *testing.T) {
	url := setupMigrateTestDB(t)

	msg, err := RunMigrate(url, []string{"version"})
	if err != nil {
		t.Fatalf("version on empty db: %v", err)
	}
	if msg != "no version (empty database)" {
		t.Errorf("expected empty database message, got %q", msg)
	}

	if _, err := RunMigrate(url, []string{"up"}); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	msg, err = RunMigrate(url, []string{"version"})
	if err != nil {
		t.Fatalf("version after up: %v", err)
	}
	want := fmt.Sprintf("version %d (dirty=false)", expectedVersion(t))
	if msg != want {
		t.Errorf("expected %q, got %q", want, msg)
	}
}

// MIG-02: down rolls back the last migration (and older ones), up re-applies
func TestMigrate_Down(t *testing.T) {
	url := setupMigrateTestDB(t)

	if _, err := RunMigrate(url, []string{"up"}); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	target := expectedVersion(t)

	// Roll back the newest migration (also exercises 000011_reconciliation.down)
	if _, err := RunMigrate(url, []string{"down"}); err != nil {
		t.Fatalf("migrate down 1: %v", err)
	}
	msg, err := RunMigrate(url, []string{"version"})
	if err != nil {
		t.Fatalf("version after down: %v", err)
	}
	want := fmt.Sprintf("version %d (dirty=false)", target-1)
	if msg != want {
		t.Errorf("expected %q after down, got %q", want, msg)
	}

	// Roll back a second step (000011.down.sql must exist for this)
	if _, err := RunMigrate(url, []string{"down", "1"}); err != nil {
		t.Fatalf("migrate down second step: %v", err)
	}
	msg, _ = RunMigrate(url, []string{"version"})
	if want := fmt.Sprintf("version %d (dirty=false)", target-2); msg != want {
		t.Errorf("expected %q after second down, got %q", want, msg)
	}

	// Re-apply to latest
	if _, err := RunMigrate(url, []string{"up"}); err != nil {
		t.Fatalf("migrate up after down: %v", err)
	}
	msg, _ = RunMigrate(url, []string{"version"})
	if want := fmt.Sprintf("version %d (dirty=false)", target); msg != want {
		t.Errorf("expected %q after re-up, got %q", want, msg)
	}
}
