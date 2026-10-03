//go:build perf

package trader

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type perfFakeFills struct{}

func (perfFakeFills) FetchTraderFills(context.Context, string, int64, int64) ([]Fill, bool, error) {
	return nil, false, nil
}

func perfN(t *testing.T) int {
	t.Helper()
	if v := os.Getenv("PERF_WALLETS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 100000
}

func perfPool(t *testing.T) *pgxpool.Pool {
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

// seedSynthetic inserts n deterministic wallets + 30D period rows (fixed
// seed 42). display_name='perf-seed' marks cleanup scope (cascade removes
// period rows).
func seedSynthetic(t *testing.T, pool *pgxpool.Pool, venueID uuid.UUID, n int) {
	t.Helper()
	ctx := context.Background()
	rng := rand.New(rand.NewSource(42))
	addrs := make([]string, n)
	pnls, wrs, vols := make([]*float64, n), make([]*float64, n), make([]*float64, n)
	tcs := make([]*int64, n)
	lts := make([]time.Time, n)
	for i := range addrs {
		addrs[i] = fmt.Sprintf("0xf00%037x", i+1)
		if rng.Intn(10) == 0 {
			continue // 10% NULL-metric rows
		}
		p := rng.ExpFloat64()*20000 - 5000
		w := rng.Float64() * 100
		v := rng.Float64() * 1000000
		tc := int64(rng.Intn(500))
		pnls[i], wrs[i], vols[i], tcs[i] = &p, &w, &v, &tc
		lts[i] = time.Now().UTC().Add(-time.Duration(rng.Intn(72)) * time.Hour)
	}
	if _, err := pool.Exec(ctx,
		`DELETE FROM trader_registry WHERE venue_id = $1 AND display_name = 'perf-seed'`, venueID); err != nil {
		t.Fatalf("pre-clean: %v", err)
	}
	if _, err := pool.CopyFrom(ctx,
		pgx.Identifier{"trader_registry"},
		[]string{"venue_id", "wallet_address", "discovery_source", "display_name"},
		pgx.CopyFromSlice(len(addrs), func(i int) ([]any, error) {
			return []any{venueID, addrs[i], "leaderboard", "perf-seed"}, nil
		})); err != nil {
		t.Fatalf("seed registry: %v", err)
	}
	rows := make([][]any, n)
	for i := range addrs {
		var lt any
		if !lts[i].IsZero() {
			lt = lts[i]
		}
		rows[i] = []any{venueID, addrs[i], "30D", time.Now().UTC(),
			pnls[i], wrs[i], vols[i], tcs[i], lt, "ready", 1}
	}
	if _, err := pool.CopyFrom(ctx,
		pgx.Identifier{"trader_period_metrics"},
		[]string{"venue_id", "wallet_address", "period", "as_of", "pnl", "win_rate",
			"volume", "trade_count", "last_trade_at", "data_status", "calculation_version"},
		pgx.CopyFromRows(rows)); err != nil {
		t.Fatalf("seed periods: %v", err)
	}
	t.Logf("seeded %d wallets", n)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM trader_registry WHERE venue_id = $1 AND display_name = 'perf-seed'`, venueID)
	})
}

func percentile(durs []time.Duration, p float64) time.Duration {
	if len(durs) == 0 {
		return 0
	}
	cp := append([]time.Duration(nil), durs...)
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
	return cp[int(p*float64(len(cp)-1))]
}

// PERF-01: scanner p95 over a mixed workload + index-usage guard.
func TestPerf_Scanner(t *testing.T) {
	n := perfN(t)
	ctx := context.Background()
	pool := perfPool(t)
	repo := NewRepository(pool)
	venueID, err := repo.VenueIDByCode(ctx, "hyperliquid")
	if err != nil {
		t.Fatalf("venue: %v", err)
	}
	seedSynthetic(t, pool, venueID, n)
	svc := NewService(repo, nil, testSecretPerf())

	queries := []SearchRequest{
		{Period: Period30D, SortBy: "pnl", SortDirection: "desc", Limit: 50},
		{Period: Period30D, SortBy: "win_rate", SortDirection: "desc", Limit: 50},
		{Period: Period30D, SortBy: "volume", SortDirection: "desc", Limit: 50},
		{Period: Period30D, SortBy: "roi", SortDirection: "asc", Limit: 50},
		{Period: Period30D, SortBy: "trade_count", SortDirection: "desc", Limit: 50},
	}
	var durs []time.Duration
	for i := 0; i < 50; i++ {
		base := queries[i%len(queries)]
		req := base
		req.Venue = "hyperliquid"
		if i%3 == 0 {
			v := 0.0
			req.PnLMin = &v
		}
		if i%4 == 0 {
			w := 50.0
			req.WinRateMin = &w
		}
		start := time.Now()
		res, err := svc.Search(ctx, uuid.Nil, &req)
		if err != nil {
			t.Fatalf("query %d: %v", i, err)
		}
		_ = res
		durs = append(durs, time.Since(start))
	}
	t.Logf("scanner p50=%s p95=%s over %d queries (%d wallets)", percentile(durs, 0.5), percentile(durs, 0.95), len(durs), n)

	// Index guard: representative queries must not seq-scan period metrics.
	for name, req := range map[string]SearchRequest{
		"filter-sort": {Period: Period30D, Venue: "hyperliquid", SortBy: "pnl", SortDirection: "desc", Limit: 50},
		"cursor-page": {Period: Period30D, Venue: "hyperliquid", SortBy: "win_rate", SortDirection: "desc", Limit: 50},
	} {
		r := req
		q, args := buildSearchQuery(&r, venueID, nil, nil, "")
		var planJSON string
		if err := pool.QueryRow(ctx, "EXPLAIN (FORMAT JSON) "+q, args...).Scan(&planJSON); err != nil {
			t.Fatalf("explain %s: %v", name, err)
		}
		if containsSeqScan(planJSON) {
			t.Errorf("%s: sequential scan detected", name)
		}
	}
}

func containsSeqScan(planJSON string) bool {
	var plans []map[string]any
	if err := json.Unmarshal([]byte(planJSON), &plans); err != nil {
		return true
	}
	var walk func(n any) bool
	walk = func(n any) bool {
		switch v := n.(type) {
		case map[string]any:
			if v["Node Type"] == "Seq Scan" {
				return true
			}
			for _, c := range v {
				if walk(c) {
					return true
				}
			}
		case []any:
			for _, c := range v {
				if walk(c) {
					return true
				}
			}
		}
		return false
	}
	return walk(plans)
}

// PERF-02: sync pool throughput over 1k wallets with empty fills.
func TestPerf_SyncThroughput(t *testing.T) {
	ctx := context.Background()
	pool := perfPool(t)
	repo := NewRepository(pool)
	// Dedicated venue: SyncAll is venue-scoped and must not touch live rows.
	if _, err := pool.Exec(ctx, `
		INSERT INTO venues (code, name, venue_type)
		VALUES ('perf-venue', 'Perf Venue', 'PERP_DEX')
		ON CONFLICT (code) DO NOTHING`); err != nil {
		t.Fatalf("venue: %v", err)
	}
	venueID, err := repo.VenueIDByCode(ctx, "perf-venue")
	if err != nil {
		t.Fatalf("venue: %v", err)
	}
	const n = 1000
	addrs := make([]string, n)
	for i := range addrs {
		addrs[i] = fmt.Sprintf("0xf01%037x", i+1)
	}
	if _, err := pool.Exec(ctx,
		`DELETE FROM trader_registry WHERE venue_id = $1 AND display_name = 'perf-seed'`, venueID); err != nil {
		t.Fatalf("pre-clean: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO trader_registry (venue_id, wallet_address, discovery_source, display_name)
		SELECT $1, addr, 'leaderboard', 'perf-seed' FROM UNNEST($2::text[]) AS addr
		ON CONFLICT DO NOTHING`, venueID, addrs); err != nil {
		t.Fatalf("seed: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM trader_registry WHERE venue_id = $1 AND display_name = 'perf-seed'`, venueID)
	})
	opts := DefaultSyncOptions()
	opts.Workers = 4
	svc := NewSyncService(repo, perfFakeFills{}, venueID, opts)

	start := time.Now()
	done, failed := svc.SyncAll(ctx, time.Now().UTC())
	elapsed := time.Since(start)
	if done != n || failed != 0 {
		t.Fatalf("throughput: done=%d failed=%d", done, failed)
	}
	t.Logf("sync throughput: %d wallets in %s (%.0f/min)", n, elapsed,
		float64(n)/elapsed.Minutes())
}

// PERF-03: WS burst of 100k events through the harvest path.
func TestPerf_WSBurst(t *testing.T) {
	ctx := context.Background()
	pool := perfPool(t)
	repo := NewRepository(pool)
	venueID, err := repo.VenueIDByCode(ctx, "hyperliquid")
	if err != nil {
		t.Fatalf("venue: %v", err)
	}
	const unique = 10000
	addrs := make([]string, unique)
	for i := range addrs {
		addrs[i] = fmt.Sprintf("0xf02%037x", i+1)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM trader_registry WHERE venue_id = $1 AND wallet_address = ANY($2)`, venueID, addrs)
	})
	harvest := NewWSHarvestService(repo, venueID, 1000, 50*time.Millisecond)
	hctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go harvest.Start(hctx)

	now := time.Now().UTC()
	for b := 0; b < 100; b++ {
		batch := make([]WSTradeEvent, 0, 1000)
		for i := 0; i < 1000; i++ {
			a := addrs[(b*1000+i)%unique]
			batch = append(batch, WSTradeEvent{Coin: "BTC", Time: now, Buyer: a, Seller: a})
		}
		harvest.Submit(batch)
	}
	deadline := time.Now().Add(60 * time.Second)
	for {
		var count int
		_ = pool.QueryRow(ctx, `SELECT COUNT(*) FROM trader_registry
			WHERE venue_id = $1 AND discovery_source = 'ws_trade' AND wallet_address LIKE '0xf02%'`,
			venueID).Scan(&count)
		if count >= unique {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("burst incomplete: %d/%d", count, unique)
		}
		time.Sleep(500 * time.Millisecond)
	}
	st := harvest.Stats()
	t.Logf("ws burst: 100k events, merged=%d skipped=%d dropped=%d flushes=%d",
		st.Merged, st.Skipped, st.Dropped, st.Flushes)
	if st.Dropped != 0 {
		t.Errorf("drops under burst: %d", st.Dropped)
	}
}

func testSecretPerf() []byte { return []byte("perf-cursor-secret-32-bytes!!!!!") }
