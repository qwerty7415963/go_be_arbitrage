//go:build integration

package wallet

import (
	"context"
	"net/url"
	"testing"
)

// POS-I-01: per-market snapshots come back sorted by pnl desc (NULL pnl
// last), and 24H/30D windows stay isolated.
func TestRepo_GetPositions_SortedAndTimeframeIsolated(t *testing.T) {
	f := setupScannerFixture(t)
	ctx := context.Background()

	walletID, _ := tagFixtureWallet(t, f, 10)

	btcPnl, ethPnl := 5000.0, 9000.0
	if err := f.insertSnapshot(ctx, walletID, "BTC", Timeframe30D, &snapValues{realizedPnl: &btcPnl}, nil); err != nil {
		t.Fatalf("insert BTC: %v", err)
	}
	if err := f.insertSnapshot(ctx, walletID, "ETH", Timeframe30D, &snapValues{realizedPnl: &ethPnl}, nil); err != nil {
		t.Fatalf("insert ETH: %v", err)
	}
	if err := f.insertSnapshot(ctx, walletID, "SOL", Timeframe30D, nil, nil); err != nil {
		t.Fatalf("insert SOL (null pnl): %v", err)
	}
	// Different timeframe must not leak into 30D.
	day := 100.0
	if err := f.insertSnapshot(ctx, walletID, "OP", Timeframe24H, &snapValues{realizedPnl: &day}, nil); err != nil {
		t.Fatalf("insert OP 24H: %v", err)
	}

	positions, err := f.repo.GetPositions(ctx, walletID, Timeframe30D)
	if err != nil {
		t.Fatalf("GetPositions: %v", err)
	}
	if len(positions) != 3 {
		t.Fatalf("expected 3 positions, got %d: %+v", len(positions), positions)
	}
	if positions[0].Market != "ETH" || positions[1].Market != "BTC" || positions[2].Market != "SOL" {
		t.Errorf("order: got %s, %s, %s", positions[0].Market, positions[1].Market, positions[2].Market)
	}
	if positions[2].RealizedPnl != nil {
		t.Errorf("NULL pnl row must keep null metrics: %v", positions[2].RealizedPnl)
	}
	if positions[0].RealizedPnl == nil || *positions[0].RealizedPnl != ethPnl {
		t.Errorf("ETH pnl: %v", positions[0].RealizedPnl)
	}

	day24, err := f.repo.GetPositions(ctx, walletID, Timeframe24H)
	if err != nil {
		t.Fatalf("GetPositions 24H: %v", err)
	}
	if len(day24) != 1 || day24[0].Market != "OP" {
		t.Errorf("24H positions: %+v", day24)
	}
}

// POS-I-02: wallet without per-market snapshots → empty slice, and the
// detail response embeds positions (never null).
func TestRepo_GetPositions_EmptyAndDetailEmbeds(t *testing.T) {
	f := setupScannerFixture(t)
	ctx := context.Background()

	walletID, _ := tagFixtureWallet(t, f, 11)

	positions, err := f.repo.GetPositions(ctx, walletID, Timeframe30D)
	if err != nil {
		t.Fatalf("GetPositions: %v", err)
	}
	if positions == nil || len(positions) != 0 {
		t.Errorf("expected empty non-nil slice, got %v", positions)
	}

	detail, err := f.svc.Detail(ctx, f.userA, walletID, url.Values{})
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if detail.Positions == nil || len(detail.Positions) != 0 {
		t.Errorf("detail positions must be []: %v", detail.Positions)
	}
}

// WL-I-01: starring is idempotent (PK upsert → one row even when repeated).
func TestRepo_Watchlist_StarIdempotent(t *testing.T) {
	f := setupScannerFixture(t)
	ctx := context.Background()
	walletID, _ := tagFixtureWallet(t, f, 12)

	for i := 0; i < 2; i++ {
		if err := f.repo.SetWatchlisted(ctx, f.userA, walletID, true); err != nil {
			t.Fatalf("star %d: %v", i, err)
		}
	}

	var n int64
	if err := f.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM user_wallet_watchlist WHERE user_id = $1 AND wallet_id = $2`,
		f.userA, walletID).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("expected exactly 1 row, got %d", n)
	}
}

// WL-I-02: stars are strictly per-user — B's filter never sees A's stars,
// and B's rows never report watchlisted=true.
func TestRepo_Watchlist_PerUserIsolation(t *testing.T) {
	f := setupScannerFixture(t)
	ctx := context.Background()
	userB := tagFixtureUser(t, f, 20)
	t.Cleanup(func() {
		f.pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userB)
	})
	walletID, addr := tagFixtureWallet(t, f, 13)

	if err := f.repo.SetWatchlisted(ctx, f.userA, walletID, true); err != nil {
		t.Fatalf("star: %v", err)
	}

	// A's watchlist filter includes the wallet.
	starredA, _, err := f.svc.Scan(ctx, f.userA, url.Values{"watchlisted": {"true"}, "limit": {"200"}})
	if err != nil {
		t.Fatalf("A scan: %v", err)
	}
	foundA := false
	for _, w := range starredA {
		if w.Address == addr {
			foundA = true
			if !w.Watchlisted {
				t.Error("A row must report watchlisted=true")
			}
		}
	}
	if !foundA {
		t.Errorf("A's starred wallet missing from filter: %d rows", len(starredA))
	}

	// B's watchlist filter must not include it.
	starredB, _, err := f.svc.Scan(ctx, userB, url.Values{"watchlisted": {"true"}, "limit": {"200"}})
	if err != nil {
		t.Fatalf("B scan: %v", err)
	}
	for _, w := range starredB {
		if w.Address == addr {
			t.Error("B's filter must never see A's star")
		}
	}

	// B's unfiltered rows report watchlisted=false.
	all, _, err := f.svc.Scan(ctx, userB, url.Values{"limit": {"200"}})
	if err != nil {
		t.Fatalf("B scan all: %v", err)
	}
	for _, w := range all {
		if w.Watchlisted {
			t.Errorf("%s: B must see watchlisted=false", w.Address)
		}
	}
}

// WL-I-03: unstar deletes the row; the filter then excludes the wallet.
func TestRepo_Watchlist_UnstarDeletesRow(t *testing.T) {
	f := setupScannerFixture(t)
	ctx := context.Background()
	walletID, addr := tagFixtureWallet(t, f, 14)

	if err := f.repo.SetWatchlisted(ctx, f.userA, walletID, true); err != nil {
		t.Fatalf("star: %v", err)
	}
	if err := f.repo.SetWatchlisted(ctx, f.userA, walletID, false); err != nil {
		t.Fatalf("unstar: %v", err)
	}

	var n int64
	if err := f.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM user_wallet_watchlist WHERE user_id = $1 AND wallet_id = $2`,
		f.userA, walletID).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("expected row deleted, got %d", n)
	}

	starred, _, err := f.svc.Scan(ctx, f.userA, url.Values{"watchlisted": {"true"}, "limit": {"200"}})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	for _, w := range starred {
		if w.Address == addr {
			t.Error("unstarred wallet must be excluded from watchlisted filter")
		}
	}
}

// WL-U/scan plumbing: watchlisted=false keeps only unstarred rows.
func TestRepo_Watchlist_FalseFilterExcludesStarred(t *testing.T) {
	f := setupScannerFixture(t)
	ctx := context.Background()
	walletID, addr := tagFixtureWallet(t, f, 15)

	if err := f.repo.SetWatchlisted(ctx, f.userA, walletID, true); err != nil {
		t.Fatalf("star: %v", err)
	}

	unstarred, _, err := f.svc.Scan(ctx, f.userA, url.Values{"watchlisted": {"false"}, "limit": {"200"}})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	for _, w := range unstarred {
		if w.Address == addr {
			t.Error("starred wallet must be excluded from watchlisted=false")
		}
	}

	// Sanity: starred wallet still exists in the unfiltered scan.
	var exists bool
	if err := f.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM tracked_wallets WHERE id = $1)`, walletID).Scan(&exists); err != nil {
		t.Fatalf("exists: %v", err)
	}
	if !exists {
		t.Error("wallet vanished")
	}
}
