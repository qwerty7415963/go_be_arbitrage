//go:build integration

package wallet

import (
	"context"
	"net/url"
	"strings"
	"testing"
)

// SCAN-H-20: rows with a snapshot carry metrics.computed_at (freshness);
// rows without one carry null metrics.
func TestPerf_ScanWallets_ComputedAt(t *testing.T) {
	f := setupScannerFixture(t)

	withSnap, _ := scan(t, f, url.Values{"search": {fixtureAddr(5)}, "limit": {"10"}})
	if len(withSnap) != 1 {
		t.Fatalf("expected 1 row, got %d", len(withSnap))
	}
	if withSnap[0].Metrics == nil || withSnap[0].Metrics.ComputedAt == nil {
		t.Errorf("expected computed_at, got %+v", withSnap[0].Metrics)
	}

	withoutSnap, _ := scan(t, f, url.Values{"search": {fixtureAddr(41)}, "limit": {"10"}})
	if len(withoutSnap) != 1 {
		t.Fatalf("expected 1 row, got %d", len(withoutSnap))
	}
	if withoutSnap[0].Metrics != nil {
		t.Errorf("expected null metrics, got %+v", withoutSnap[0].Metrics)
	}
}

// HARD-04: the scanner read path is index-backed (TEST-08) — required
// indexes exist and the search predicate uses the trigram index instead of
// a sequential scan.
func TestPerf_ScannerIndexes_Used(t *testing.T) {
	f := setupScannerFixture(t)
	ctx := context.Background()

	for _, idx := range []string{
		"idx_tracked_wallets_address_trgm",
		"idx_tracked_wallets_chain",
		"uq_wallet_metrics_key",
		"idx_wallet_fills_wallet_time",
	} {
		var found bool
		err := f.pool.QueryRow(ctx, `
			SELECT EXISTS(SELECT 1 FROM pg_indexes
				WHERE schemaname = 'public' AND indexname = $1)`, idx).Scan(&found)
		if err != nil {
			t.Fatalf("index check %s: %v", idx, err)
		}
		if !found {
			t.Errorf("required scanner index missing: %s", idx)
		}
	}

	// Force the planner off sequential scans in one transaction and verify
	// the '%term%' search predicate is served by the trigram index.
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SET LOCAL enable_seqscan = off"); err != nil {
		t.Fatalf("set local: %v", err)
	}

	rows, err := tx.Query(ctx, `
		EXPLAIN SELECT w.id FROM tracked_wallets w
		WHERE w.address ILIKE '%' || $1 || '%'`, "00000a")
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	defer rows.Close()

	var plan []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan plan: %v", err)
		}
		plan = append(plan, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("plan rows: %v", err)
	}

	joined := strings.Join(plan, "\n")
	if !strings.Contains(joined, "Bitmap Index Scan") ||
		!strings.Contains(joined, "idx_tracked_wallets_address_trgm") {
		t.Errorf("search not served by trigram index:\n%s", joined)
	}
	if strings.Contains(joined, "Seq Scan") {
		t.Errorf("sequential scan in search plan:\n%s", joined)
	}
}
