package trader

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestWalletEventProjector_Classify(t *testing.T) {
	base := t0()
	venueID := uuid.New()
	addr := "0xabc"
	fills := []Fill{
		mkFill("BTC", 1, base, true, 1, 100, 0, 0),
		mkFill("BTC", 2, base.Add(time.Minute), true, 1, 101, 0, 0),
		mkFill("BTC", 3, base.Add(2*time.Minute), false, 1, 102, 0, 0),
		mkFill("BTC", 4, base.Add(3*time.Minute), false, 1, 103, 10, 0),
	}
	events := ClassifyWalletEvents(fills, venueID, addr)
	if len(events) != 4 {
		t.Fatalf("want 4 events, got %d", len(events))
	}
	want := []string{"position_opened", "position_increased", "position_reduced", "position_closed"}
	for i, e := range events {
		if e.EventType != want[i] {
			t.Errorf("event %d: got %s want %s", i, e.EventType, want[i])
		}
	}
	// dedup keys stable across repeated classification
	events2 := ClassifyWalletEvents(fills, venueID, addr)
	if len(events2) != len(events) {
		t.Fatalf("repeat count mismatch: %d vs %d", len(events2), len(events))
	}
	for i := range events {
		if events2[i].DedupKey != events[i].DedupKey {
			t.Errorf("dedup key changed on repeat")
		}
	}
}

func TestWalletEventProjector_DedupIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("short")
	}
	ctx := context.Background()
	host := os.Getenv("ARBITRAGE_DB_HOST")
	if host == "" {
		host = "localhost"
	}
	port := os.Getenv("ARBITRAGE_DB_PORT")
	if port == "" {
		port = "5433"
	}
	pool, err := pgxpool.New(ctx, fmt.Sprintf("postgres://test:test@%s:%s/arbitrage_test?sslmode=disable", host, port))
	if err != nil {
		t.Skipf("db unavailable: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("db unavailable: %v", err)
	}
	defer pool.Close()

	repo := NewRepository(pool)
	venueID := walletTestVenue(t, pool)
	addr := walletTestAddr()
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM trader_registry WHERE venue_id = $1 AND wallet_address = $2`, venueID, addr)
	})

	name := "tester"
	if _, _, err := repo.UpsertRegistry(ctx, venueID, addr, SourceManual, &name, nil); err != nil {
		t.Fatalf("registry: %v", err)
	}

	base := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	fills := []Fill{
		mkFill("BTC", 1, base, true, 1, 100, 0, 0),
		mkFill("BTC", 2, base.Add(time.Minute), true, 1, 101, 0, 0),
		mkFill("BTC", 3, base.Add(2*time.Minute), false, 1, 102, 0, 0),
		mkFill("BTC", 4, base.Add(3*time.Minute), false, 1, 103, 10, 0),
	}
	if _, err := repo.UpsertBufferFills(ctx, venueID, addr, fills); err != nil {
		t.Fatalf("buffer fills: %v", err)
	}

	if err := ProjectWalletEvents(ctx, repo, venueID, addr); err != nil {
		t.Fatalf("project: %v", err)
	}
	count1 := countWalletEvents(t, pool, venueID, addr)
	if count1 != 4 {
		t.Fatalf("want 4 events after first project, got %d", count1)
	}

	if err := ProjectWalletEvents(ctx, repo, venueID, addr); err != nil {
		t.Fatalf("project second: %v", err)
	}
	count2 := countWalletEvents(t, pool, venueID, addr)
	if count2 != count1 {
		t.Fatalf("dedup failed: %d vs %d", count2, count1)
	}

	if err := BackfillWalletEvents(ctx, repo, venueID, addr); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	count3 := countWalletEvents(t, pool, venueID, addr)
	if count3 != count1 {
		t.Fatalf("backfill changed count: %d vs %d", count3, count1)
	}
}

var walletTestAddrSeq uint64

func walletTestAddr() string {
	walletTestAddrSeq++
	n := walletTestAddrSeq
	return fmt.Sprintf("0x%040x", uint64(time.Now().UnixNano())%0xffff+n*0x10000)
}

func walletTestVenue(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
		INSERT INTO venues (code, name, venue_type)
		VALUES ('trader-test-venue', 'Trader Test Venue', 'PERP_DEX')
		ON CONFLICT (code) DO NOTHING`); err != nil {
		t.Fatalf("venue: %v", err)
	}
	var id uuid.UUID
	if err := pool.QueryRow(ctx,
		`SELECT id FROM venues WHERE code = 'trader-test-venue'`).Scan(&id); err != nil {
		t.Fatalf("venue id: %v", err)
	}
	return id
}

func countWalletEvents(t *testing.T, pool *pgxpool.Pool, venueID uuid.UUID, addr string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM trader_wallet_event WHERE venue_id = $1 AND wallet_address = $2`,
		venueID, addr).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}
