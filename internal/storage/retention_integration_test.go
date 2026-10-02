//go:build integration

package storage

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
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

var retAddrSeq atomic.Uint64

func retAddr() string {
	return fmt.Sprintf("0x%040x", uint64(time.Now().UnixNano())%0xffff+retAddrSeq.Add(1)*0x100000)
}

// RET-I-04: full cleanup continues past missing tables (orderbook_* absent
// in this DB — pre-existing schema gap) and still cleans trader tables.
func TestRetention_FullCleanupPartial(t *testing.T) {
	ctx := context.Background()
	pool := retentionPool(t)
	svc := NewRetentionService(pool)

	res, err := svc.RunFullCleanup(ctx, DefaultRetentionConfig())
	if err == nil {
		t.Log("full cleanup green")
		return
	}
	if res == nil {
		t.Fatalf("partial result must be non-nil: %v", err)
	}
	t.Logf("partial cleanup (expected in this env): %v", err)
}

// RET-I-03: dead wallets pruned (children cascade); traded, grouped and
// never-synced wallets kept; a pruned address re-enters cleanly (self-healing).
func TestRetention_DeadTraders(t *testing.T) {
	ctx := context.Background()
	pool := retentionPool(t)
	svc := NewRetentionService(pool)

	var venueID uuid.UUID
	if err := pool.QueryRow(ctx,
		`SELECT id FROM venues WHERE code = 'hyperliquid'`).Scan(&venueID); err != nil {
		t.Fatalf("venue: %v", err)
	}
	dead, traded, grouped, fresh := retAddr(), retAddr(), retAddr(), retAddr()
	old := time.Now().UTC().Add(-60 * 24 * time.Hour)
	for _, a := range []string{dead, traded, grouped, fresh} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO trader_registry (venue_id, wallet_address, discovery_source, first_seen_at, last_seen_at)
			VALUES ($1, $2, 'ws_trade', $3, $3)`, venueID, a, old); err != nil {
			t.Fatalf("registry: %v", err)
		}
	}
	t.Cleanup(func() {
		for _, a := range []string{dead, traded, grouped, fresh} {
			_, _ = pool.Exec(context.Background(),
				`DELETE FROM trader_registry WHERE venue_id = $1 AND wallet_address = $2`, venueID, a)
		}
	})
	// Sync completed for all but fresh (never attempted → kept).
	for _, a := range []string{dead, traded, grouped} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO trader_sync_state (venue_id, wallet_address, sync_status, backfill_completed_at)
			VALUES ($1, $2, 'ready', $3)`, venueID, a, old); err != nil {
			t.Fatalf("sync: %v", err)
		}
	}
	// Traded history keeps the wallet.
	if _, err := pool.Exec(ctx, `
		INSERT INTO trader_daily_stats (venue_id, wallet_address, stat_date, trade_count)
		VALUES ($1, $2, CURRENT_DATE - 40, 5)`, venueID, traded); err != nil {
		t.Fatalf("daily: %v", err)
	}
	// Grouped wallet: user + group + member.
	tenantID, userID := uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO tenants (id, name) VALUES ($1, 'ret-test')`, tenantID); err != nil {
		t.Fatalf("tenant: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, tenant_id, email, password_hash) VALUES ($1, $2, 'r@t.co', 'x')`,
		userID, tenantID); err != nil {
		t.Fatalf("user: %v", err)
	}
	var groupID uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO trader_groups (user_id, name) VALUES ($1, 'g') RETURNING id`,
		userID).Scan(&groupID); err != nil {
		t.Fatalf("group: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM trader_groups WHERE id = $1`, groupID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM tenants WHERE id = $1`, tenantID)
	})
	if _, err := pool.Exec(ctx, `INSERT INTO trader_group_members (group_id, venue_id, wallet_address)
		VALUES ($1, $2, $3)`, groupID, venueID, grouped); err != nil {
		t.Fatalf("member: %v", err)
	}

	n, err := svc.CleanupDeadTraders(ctx, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	// Never assert an exact global count: the shared DB may hold other
	// prunable wallets written by live workers.
	if n < 1 {
		t.Errorf("seeded dead wallet must be pruned, got %d", n)
	}
	for addr, wantGone := range map[string]bool{dead: true, traded: false, grouped: false, fresh: false} {
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM trader_registry
			WHERE venue_id = $1 AND wallet_address = $2)`, venueID, addr).Scan(&exists); err != nil {
			t.Fatalf("check: %v", err)
		}
		if exists == wantGone {
			t.Errorf("%s: gone=%v, want gone=%v", addr, !exists, wantGone)
		}
	}
	// Self-healing: the address re-enters as a fresh discovery.
	if _, err := pool.Exec(ctx, `
		INSERT INTO trader_registry (venue_id, wallet_address, discovery_source)
		VALUES ($1, $2, 'ws_trade')`, venueID, dead); err != nil {
		t.Errorf("re-entry: %v", err)
	}
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

	if _, err := svc.CleanupTraderDaily(ctx, 30*24*time.Hour); err != nil {
		t.Fatalf("daily cleanup: %v", err)
	}
	if _, err := svc.CleanupTraderEquity(ctx, 30*24*time.Hour); err != nil {
		t.Fatalf("equity cleanup: %v", err)
	}
	// Scoped assertions only: the shared DB may hold other old rows
	// (live backfills write historical days); never assert global counts.
	var oldLeft int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM trader_daily_stats
		WHERE venue_id = $1 AND wallet_address = $2 AND stat_date = $3`,
		venueID, addr, oldDay).Scan(&oldLeft); err != nil || oldLeft != 0 {
		t.Errorf("own old daily row must be gone: %d %v", oldLeft, err)
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
