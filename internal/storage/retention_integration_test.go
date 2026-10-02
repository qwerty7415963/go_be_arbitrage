//go:build integration

package storage

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func retentionPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	host := os.Getenv("ARBITRAGE_DB_HOST")
	if host == "" {
		host = "localhost"
	}
	port := os.Getenv("ARBITRAGE_DB_PORT")
	if port == "" {
		port = "5433"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx,
		fmt.Sprintf("postgres://test:test@%s:%s/arbitrage_test?sslmode=disable", host, port))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}
	return pool
}

// RET-I-02: trader daily/equity rows past retention are deleted, recent kept.
func TestRetention_TraderDailyEquity(t *testing.T) {
	ctx := context.Background()
	pool := retentionPool(t)
	svc := NewRetentionService(pool)

	var venueID uuid.UUID
	if err := pool.QueryRow(ctx,
		`SELECT id FROM venues WHERE code = 'hyperliquid'`).Scan(&venueID); err != nil {
		t.Fatalf("venue: %v", err)
	}
	addr := fmt.Sprintf("0x%040x", uint64(time.Now().UnixNano())%0xffffff+0x900000)
	if _, err := pool.Exec(ctx, `
		INSERT INTO trader_registry (venue_id, wallet_address, discovery_source)
		VALUES ($1, $2, 'manual') ON CONFLICT DO NOTHING`, venueID, addr); err != nil {
		t.Fatalf("registry: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM trader_registry WHERE venue_id = $1 AND wallet_address = $2`, venueID, addr)
	})
	oldDay := time.Now().UTC().Add(-60 * 24 * time.Hour).Format("2006-01-02")
	newDay := time.Now().UTC().Add(-24 * time.Hour).Format("2006-01-02")
	for _, day := range []string{oldDay, newDay} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO trader_daily_stats (venue_id, wallet_address, stat_date)
			VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, venueID, addr, day); err != nil {
			t.Fatalf("daily: %v", err)
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO trader_equity_daily (venue_id, wallet_address, stat_date)
			VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, venueID, addr, day); err != nil {
			t.Fatalf("equity: %v", err)
		}
	}

	if n, err := svc.CleanupTraderDaily(ctx, 30*24*time.Hour); err != nil || n != 1 {
		t.Errorf("daily cleanup: n=%d err=%v", n, err)
	}
	if n, err := svc.CleanupTraderEquity(ctx, 30*24*time.Hour); err != nil || n != 1 {
		t.Errorf("equity cleanup: n=%d err=%v", n, err)
	}
	var remaining int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM trader_daily_stats
		WHERE venue_id = $1 AND wallet_address = $2`, venueID, addr).Scan(&remaining); err != nil || remaining != 1 {
		t.Errorf("daily remaining: %d %v", remaining, err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM trader_equity_daily
		WHERE venue_id = $1 AND wallet_address = $2`, venueID, addr).Scan(&remaining); err != nil || remaining != 1 {
		t.Errorf("equity remaining: %d %v", remaining, err)
	}
}
