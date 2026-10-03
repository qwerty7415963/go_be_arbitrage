//go:build integration

package collector

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/qwerty7415963/go_be_arbitrage/internal/logger"
	"github.com/qwerty7415963/go_be_arbitrage/internal/venue"
)

func fundingPool(t *testing.T) *pgxpool.Pool {
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

// FUND-I-01: unchanged rates store nothing; changed rate and stale heartbeat store.
func TestStoreFundingWithDiscovery_StoreOnChange(t *testing.T) {
	ctx := context.Background()
	pool := fundingPool(t)
	c := &Collector{
		db:        pool,
		venueRepo: venue.NewRepository(pool),
		logger:    logger.New("error", "text"),
		interval:  time.Minute,
	}
	var venueID uuid.UUID
	if err := pool.QueryRow(ctx,
		`SELECT id FROM venues WHERE code = 'binance'`).Scan(&venueID); err != nil {
		t.Fatalf("venue: %v", err)
	}
	const symbol, base, quote = "TESTFUNDUSDT", "TESTFUND", "USDT"
	now := time.Now().UTC()
	store := func(rate string, at time.Time) bool {
		return c.storeFundingWithDiscovery(ctx, venueID, "binance", symbol,
			base, quote, rate, 28800, "100", "100", "1000", at)
	}
	count := func() int {
		var n int
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM funding_rates fr
			JOIN instruments i ON i.id = fr.instrument_id
			WHERE fr.venue_id = $1 AND i.canonical_symbol LIKE 'TESTFUND%'`,
			venueID).Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		return n
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM funding_rates WHERE instrument_id IN
			(SELECT id FROM instruments WHERE canonical_symbol LIKE 'TESTFUND%')`)
	})

	if !store("0.0001", now) || count() != 1 {
		t.Fatalf("first sighting must store, count=%d", count())
	}
	if store("0.0001", now.Add(time.Minute)) || count() != 1 {
		t.Fatalf("unchanged rate must skip, count=%d", count())
	}
	if !store("0.0002", now.Add(2*time.Minute)) || count() != 2 {
		t.Fatalf("changed rate must store, count=%d", count())
	}
	// Backdate all rows past the heartbeat, same rate stores again.
	if _, err := pool.Exec(ctx, `UPDATE funding_rates SET observed_at = $1
		WHERE venue_id = $2 AND instrument_id IN
		(SELECT id FROM instruments WHERE canonical_symbol LIKE 'TESTFUND%')`,
		now.Add(-2*time.Hour), venueID); err != nil {
		t.Fatalf("backdate: %v", err)
	}
	if !store("0.0002", now.Add(3*time.Minute)) || count() != 3 {
		t.Fatalf("stale heartbeat must store, count=%d", count())
	}
}
