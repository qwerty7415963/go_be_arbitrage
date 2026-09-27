//go:build integration

package wallet

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type fixture struct {
	pool      *pgxpool.Pool
	repo      *Repository
	svc       *Service
	tenantID  uuid.UUID
	userA     uuid.UUID
	addrs     []string
	venueIDs  map[string]uuid.UUID
	walletIDs map[string]uuid.UUID // address -> id
}

const fixtureWallets = 50

func fixtureAddr(i int) string {
	return fmt.Sprintf("0x%040x", i+1)
}

func setupScannerFixture(t *testing.T) *fixture {
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

	dsn := fmt.Sprintf("postgres://test:test@%s:%s/arbitrage_test?sslmode=disable", host, port)
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("ping (is docker running?): %v", err)
	}

	f := &fixture{
		pool:      pool,
		repo:      NewRepository(pool),
		tenantID:  uuid.New(),
		userA:     uuid.New(),
		venueIDs:  map[string]uuid.UUID{},
		walletIDs: map[string]uuid.UUID{},
	}
	f.svc = NewService(f.repo, testConfig())

	run := time.Now().UnixNano()
	if _, err := pool.Exec(ctx,
		`INSERT INTO tenants (id, name) VALUES ($1, $2)`,
		f.tenantID, fmt.Sprintf("scan-fixture-%d", run)); err != nil {
		pool.Close()
		t.Fatalf("create tenant: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, tenant_id, email, password_hash) VALUES ($1, $2, $3, 'test-hash')`,
		f.userA, f.tenantID, fmt.Sprintf("scan-fixture-%d@test.com", run)); err != nil {
		pool.Close()
		t.Fatalf("create user: %v", err)
	}

	// Venues for dex filters (upsert by code).
	for _, code := range []string{"hyperliquid", "gmx", "extended"} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO venues (code, name, venue_type)
			VALUES ($1, $2, 'PERP_DEX')
			ON CONFLICT (code) DO NOTHING`, code, code); err != nil {
			pool.Close()
			t.Fatalf("create venue: %v", err)
		}
		var id uuid.UUID
		if err := pool.QueryRow(ctx, `SELECT id FROM venues WHERE code = $1`, code).Scan(&id); err != nil {
			pool.Close()
			t.Fatalf("load venue: %v", err)
		}
		f.venueIDs[code] = id
	}

	now := time.Now().UTC()
	for i := 0; i < fixtureWallets; i++ {
		addr := fixtureAddr(i)
		chain := "evm"
		if i%2 == 1 {
			chain = "starknet"
		}
		venue := []string{"hyperliquid", "gmx", "extended"}[i%3]

		var id uuid.UUID
		err := pool.QueryRow(ctx, `
			INSERT INTO tracked_wallets (chain, address, venue_id)
			VALUES ($1, $2, $3)
			ON CONFLICT (chain, address) DO UPDATE SET last_seen_at = NOW()
			RETURNING id`, chain, addr, f.venueIDs[venue]).Scan(&id)
		if err != nil {
			pool.Close()
			t.Fatalf("create wallet %d: %v", i, err)
		}
		f.walletIDs[addr] = id
		f.addrs = append(f.addrs, addr)

		// 30D aggregate snapshot: deterministic metrics per index.
		// i == 40 → all-NULL snapshot (SCAN-I-09); i == 41 → no snapshot.
		market := ""
		if i%5 == 0 {
			market = "BTC"
		}
		if i == 40 {
			if err := f.insertSnapshot(ctx, id, market, Timeframe30D, nil, nil); err != nil {
				pool.Close()
				t.Fatalf("null snapshot: %v", err)
			}
			continue
		}
		if i == 41 {
			continue // no snapshot at all
		}
		pnl := float64(i * 100)
		volume := float64(i * 1000)
		winRate := float64(50 + i%50)
		trades := int64(i + 1)
		if err := f.insertSnapshot(ctx, id, market, Timeframe30D, &snapValues{
			realizedPnl: &pnl,
			volume:      &volume,
			winRate:     &winRate,
			tradeCount:  &trades,
			longCount:   &trades,
			shortCount:  int64Ptr(int64(i % 3)),
		}, now.Add(-time.Duration(i)*time.Hour)); err != nil {
			pool.Close()
			t.Fatalf("snapshot: %v", err)
		}

		// 24H snapshot for the first 10 wallets only (SCAN-I-06).
		if i < 10 {
			pnl24 := 777.0
			vol24 := 7770.0
			tr24 := int64(3)
			if err := f.insertSnapshot(ctx, id, market, Timeframe24H, &snapValues{
				realizedPnl: &pnl24,
				volume:      &vol24,
				tradeCount:  &tr24,
				longCount:   &tr24,
				shortCount:  int64Ptr(1),
			}, now.Add(-2*time.Hour)); err != nil {
				pool.Close()
				t.Fatalf("24h snapshot: %v", err)
			}
		}
	}

	t.Cleanup(func() {
		ctx := context.Background()
		pool.Exec(ctx, `
			DELETE FROM wallet_metric_snapshots WHERE wallet_id IN (
				SELECT id FROM tracked_wallets WHERE address = ANY($1))`, f.addrs)
		pool.Exec(ctx, `
			DELETE FROM group_wallet_members WHERE wallet_id IN (
				SELECT id FROM tracked_wallets WHERE address = ANY($1))`, f.addrs)
		pool.Exec(ctx, `DELETE FROM user_wallet_groups WHERE user_id = $1`, f.userA)
		pool.Exec(ctx, `DELETE FROM tracked_wallets WHERE address = ANY($1)`, f.addrs)
		pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, f.userA)
		pool.Exec(ctx, `DELETE FROM tenants WHERE id = $1`, f.tenantID)
		pool.Close()
	})

	return f
}

type snapValues struct {
	realizedPnl *float64
	roi         *float64
	winRate     *float64
	volume      *float64
	tradeCount  *int64
	longCount   *int64
	shortCount  *int64
}

func int64Ptr(v int64) *int64 { return &v }

func (f *fixture) insertSnapshot(ctx context.Context, walletID uuid.UUID, market, timeframe string, v *snapValues, lastActive any) error {
	var (
		pnl, roi, wr, vol, ap, al = any(nil), any(nil), any(nil), any(nil), any(nil), any(nil)
		tc, lc, sc                = any(nil), any(nil), any(nil)
	)
	if v != nil {
		if v.realizedPnl != nil {
			pnl = *v.realizedPnl
		}
		if v.roi != nil {
			roi = *v.roi
		}
		if v.winRate != nil {
			wr = *v.winRate
		}
		if v.volume != nil {
			vol = *v.volume
		}
		if v.tradeCount != nil {
			tc = *v.tradeCount
		}
		if v.longCount != nil {
			lc = *v.longCount
		}
		if v.shortCount != nil {
			sc = *v.shortCount
		}
	}
	m := any(nil)
	if market != "" {
		m = market
	}
	_, err := f.pool.Exec(ctx, `
		INSERT INTO wallet_metric_snapshots
		    (wallet_id, market, timeframe, realized_pnl, roi, win_rate, volume,
		     trade_count, avg_position, avg_leverage, long_count, short_count,
		     last_active_at, computed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, NOW())
		ON CONFLICT DO NOTHING`,
		walletID, m, timeframe, pnl, roi, wr, vol, tc, ap, al, lc, sc, lastActive)
	return err
}

func scan(t *testing.T, f *fixture, q url.Values) ([]*Wallet, int64) {
	t.Helper()
	wallets, meta, err := f.svc.Scan(context.Background(), q)
	if err != nil {
		t.Fatalf("scan %v: %v", q, err)
	}
	total := int64(len(wallets))
	if meta.HasMore {
		total = int64(meta.TotalPages) // informational only
	}
	return wallets, total
}

func addrSet(wallets []*Wallet) map[string]bool {
	out := map[string]bool{}
	for _, w := range wallets {
		out[w.Address] = true
	}
	return out
}

// ─── SCAN-I-01: fixture + numeric filter → only matching rows ────

func TestRepo_ScanWallets_NumericFilter(t *testing.T) {
	f := setupScannerFixture(t)

	wallets, _ := scan(t, f, url.Values{"pnl_gt": {"1000"}, "limit": {"200"}})
	if len(wallets) == 0 {
		t.Fatal("expected matching wallets")
	}
	for _, w := range wallets {
		if w.Metrics == nil || w.Metrics.RealizedPnl == nil || *w.Metrics.RealizedPnl <= 1000 {
			t.Errorf("wallet %s pnl=%v violates pnl_gt=1000", w.Address, w.Metrics)
		}
	}
}

// ─── SCAN-I-02: AND combination → intersection ───────────────────

func TestRepo_ScanWallets_AndCombination(t *testing.T) {
	f := setupScannerFixture(t)

	wallets, _ := scan(t, f, url.Values{
		"pnl_gt":       {"2000"},
		"win_rate_gte": {"80"},
		"limit":        {"200"},
	})
	for _, w := range wallets {
		if *w.Metrics.RealizedPnl <= 2000 {
			t.Errorf("%s: pnl violation", w.Address)
		}
		if *w.Metrics.WinRate < 80 {
			t.Errorf("%s: win_rate violation", w.Address)
		}
	}

	// Intersection must be strictly smaller than each side alone.
	pnlOnly, _ := scan(t, f, url.Values{"pnl_gt": {"2000"}, "limit": {"200"}})
	if len(wallets) >= len(pnlOnly) && len(pnlOnly) > 0 {
		t.Errorf("AND (%d) should be <= pnl-only (%d)", len(wallets), len(pnlOnly))
	}
}

// ─── SCAN-I-03: multi-select OR → union ──────────────────────────

func TestRepo_ScanWallets_MultiSelectOr(t *testing.T) {
	f := setupScannerFixture(t)

	wallets, _ := scan(t, f, url.Values{"dex": {"hyperliquid,gmx"}, "limit": {"200"}})
	seenDex := map[string]bool{}
	seenAddr := map[string]bool{}
	for _, w := range wallets {
		if w.Dex != "hyperliquid" && w.Dex != "gmx" {
			t.Errorf("unexpected dex %q", w.Dex)
		}
		seenDex[w.Dex] = true
		seenAddr[w.Address] = true
	}
	if !seenDex["hyperliquid"] || !seenDex["gmx"] {
		t.Errorf("expected union of both dexes, got %v", seenDex)
	}

	// Each side alone is a subset of the union.
	hyper, _ := scan(t, f, url.Values{"dex": {"hyperliquid"}, "limit": {"200"}})
	for _, w := range hyper {
		if !seenAddr[w.Address] {
			t.Errorf("%s missing from union", w.Address)
		}
	}
}

// ─── SCAN-I-04: search canonical match (casing) ──────────────────

func TestRepo_ScanWallets_SearchCaseInsensitive(t *testing.T) {
	f := setupScannerFixture(t)
	target := fixtureAddr(7)
	hex := target[2:] // without 0x

	for _, q := range []url.Values{
		{"search": {target}},
		{"search": {"0X" + hex}},
		{"search": {hex}},
	} {
		wallets, _ := scan(t, f, q)
		set := addrSet(wallets)
		if !set[target] {
			t.Errorf("search %v: expected %s in %v", q, target, set)
		}
	}
}

// ─── SCAN-I-05: sort + page walk → deterministic, no dup/skip ────

func TestRepo_ScanWallets_SortPageWalk_Deterministic(t *testing.T) {
	f := setupScannerFixture(t)

	asc, _ := scan(t, f, url.Values{"sort": {"pnl"}, "order": {"asc"}, "limit": {"200"}})
	if len(asc) < 10 {
		t.Fatalf("expected fixture rows, got %d", len(asc))
	}
	// Rows with metrics sort first (NULLS LAST); skip nil-metric rows.
	ascWith := asc[:0]
	for _, w := range asc {
		if w.Metrics != nil && w.Metrics.RealizedPnl != nil {
			ascWith = append(ascWith, w)
		}
	}
	for i := 1; i < len(ascWith); i++ {
		prev, cur := ascWith[i-1].Metrics.RealizedPnl, ascWith[i].Metrics.RealizedPnl
		if *prev > *cur {
			t.Fatalf("ascending order violated at %d: %v > %v", i, *prev, *cur)
		}
	}
	desc, _ := scan(t, f, url.Values{"sort": {"pnl"}, "order": {"desc"}, "limit": {"200"}})
	descWith := desc[:0]
	for _, w := range desc {
		if w.Metrics != nil && w.Metrics.RealizedPnl != nil {
			descWith = append(descWith, w)
		}
	}
	for i := 1; i < len(descWith); i++ {
		if *descWith[i-1].Metrics.RealizedPnl < *descWith[i].Metrics.RealizedPnl {
			t.Fatalf("descending order violated at %d", i)
		}
	}

	// Page walk with limit 7: no duplicates, no skips vs single page.
	paged := map[string]int{}
	for page := 1; ; page++ {
		rows, _ := scan(t, f, url.Values{"sort": {"pnl"}, "order": {"asc"}, "limit": {"7"}, "page": {fmt.Sprint(page)}})
		if len(rows) == 0 {
			break
		}
		for _, w := range rows {
			paged[w.Address]++
		}
		if page > 30 {
			t.Fatal("page walk did not terminate")
		}
	}
	if len(paged) != len(asc) {
		t.Errorf("page walk covered %d wallets, single page %d", len(paged), len(asc))
	}
	for addr, n := range paged {
		if n != 1 {
			t.Errorf("wallet %s appeared %d times", addr, n)
		}
	}

	// Deterministic tiebreak: equal pnl → ordered by (chain, address).
	for i := 1; i < len(ascWith); i++ {
		if *ascWith[i-1].Metrics.RealizedPnl == *ascWith[i].Metrics.RealizedPnl {
			if ascWith[i-1].Chain > ascWith[i].Chain ||
				(ascWith[i-1].Chain == ascWith[i].Chain && ascWith[i-1].Address > ascWith[i].Address) {
				t.Errorf("tiebreak violated between %s and %s", ascWith[i-1].Address, ascWith[i].Address)
			}
		}
	}
}

// ─── SCAN-I-06: timeframe scoping (24H vs 30D/ALL) ───────────────

func TestRepo_ScanWallets_TimeframeScoped(t *testing.T) {
	f := setupScannerFixture(t)

	// Wallet 0 has both 24H and 30D snapshots.
	addrWith24 := fixtureAddr(0)
	// Wallet 20 has only 30D.
	addr30Only := fixtureAddr(20)

	w24, _ := scan(t, f, url.Values{"timeframe": {"24H"}, "limit": {"200"}})
	byAddr := addrSet(w24)
	if !byAddr[addrWith24] {
		t.Error("expected wallet with 24H snapshot")
	}
	for _, w := range w24 {
		if w.Address == addrWith24 && (*w.Metrics.RealizedPnl != 777) {
			t.Errorf("24H pnl: expected 777, got %v", *w.Metrics.RealizedPnl)
		}
		if w.Address == addr30Only && w.Metrics != nil {
			t.Errorf("24H must not expose 30D metrics, got %+v", w.Metrics)
		}
	}

	w30, _ := scan(t, f, url.Values{"timeframe": {"30D"}, "limit": {"200"}})
	for _, w := range w30 {
		if w.Address == addrWith24 && *w.Metrics.RealizedPnl != 0 { // i=0 → pnl 0
			t.Errorf("30D pnl: expected 0, got %v", *w.Metrics.RealizedPnl)
		}
		if w.Address == addr30Only && w.Metrics == nil {
			t.Error("30D must expose 30D metrics")
		}
	}
}

// ─── SCAN-I-07: GetDetail consistent with scanner (same timeframe) ─

func TestRepo_GetDetail_MatchesScanner(t *testing.T) {
	f := setupScannerFixture(t)
	addr := fixtureAddr(5)

	scanned, _ := scan(t, f, url.Values{"search": {addr}, "timeframe": {"30D"}})
	if len(scanned) != 1 {
		t.Fatalf("expected 1 wallet, got %d", len(scanned))
	}

	detail, err := f.svc.Detail(context.Background(), f.userA, scanned[0].ID, url.Values{"timeframe": {"30D"}})
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if detail.Address != addr {
		t.Errorf("address mismatch: %s vs %s", detail.Address, addr)
	}
	if detail.Metrics == nil || scanned[0].Metrics == nil {
		t.Fatal("expected metrics on both sides")
	}
	if *detail.Metrics.RealizedPnl != *scanned[0].Metrics.RealizedPnl {
		t.Errorf("pnl mismatch: detail %v vs scan %v",
			*detail.Metrics.RealizedPnl, *scanned[0].Metrics.RealizedPnl)
	}
	if *detail.Metrics.WinRate != *scanned[0].Metrics.WinRate {
		t.Errorf("win_rate mismatch")
	}
}

// ─── SCAN-I-08: group filter → only group rows, others untouched ─

func TestRepo_ScanGroupWallets_FilterSubset(t *testing.T) {
	f := setupScannerFixture(t)
	ctx := context.Background()

	var groupID uuid.UUID
	err := f.pool.QueryRow(ctx, `
		INSERT INTO user_wallet_groups (id, user_id, name)
		VALUES ($1, $2, 'Scanner Group')
		RETURNING id`, uuid.New(), f.userA).Scan(&groupID)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}

	// 10 wallets in the group (i = 0..9 → pnl 0..900).
	groupAddrs := map[string]bool{}
	for i := 0; i < 10; i++ {
		addr := fixtureAddr(i)
		groupAddrs[addr] = true
		if _, err := f.pool.Exec(ctx, `
			INSERT INTO group_wallet_members (group_id, wallet_id, added_by)
			VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
			groupID, f.walletIDs[addr], f.userA); err != nil {
			t.Fatalf("add member: %v", err)
		}
	}

	// Filter matching only i = 5..9 (pnl_gt=400 → i*100 > 400 → i >= 5).
	wallets, _, err := f.svc.ScanGroupWallets(ctx, f.userA, groupID,
		url.Values{"pnl_gt": {"400"}, "limit": {"200"}})
	if err != nil {
		t.Fatalf("scan group: %v", err)
	}
	if len(wallets) != 5 {
		t.Fatalf("expected 5 rows, got %d", len(wallets))
	}
	for _, w := range wallets {
		if !groupAddrs[w.Address] {
			t.Errorf("%s is not in the group", w.Address)
		}
		if *w.Metrics.RealizedPnl <= 400 {
			t.Errorf("%s violates filter", w.Address)
		}
	}

	// Membership unchanged: full group still has 10 wallets.
	all, _, err := f.svc.ScanGroupWallets(ctx, f.userA, groupID, url.Values{"limit": {"200"}})
	if err != nil {
		t.Fatalf("scan group all: %v", err)
	}
	if len(all) != 10 {
		t.Errorf("membership changed: expected 10 rows, got %d", len(all))
	}

	// Ownership: foreign user → GROUP-003, unknown group → GROUP-001.
	if _, _, err := f.svc.ScanGroupWallets(ctx, uuid.New(), groupID, url.Values{}); err == nil {
		t.Error("expected foreign-user rejection")
	}
	if _, _, err := f.svc.ScanGroupWallets(ctx, f.userA, uuid.New(), url.Values{}); err == nil {
		t.Error("expected unknown-group rejection")
	}
}

// ─── SCAN-I-09: NULL snapshot excluded by numeric filter (BR-07) ─

func TestRepo_ScanWallets_NullMetrics_ExcludedByNumericFilter(t *testing.T) {
	f := setupScannerFixture(t)

	// i=40 → all-NULL snapshot; i=41 → no snapshot.
	nullSnap := fixtureAddr(40)
	noSnap := fixtureAddr(41)

	wallets, _ := scan(t, f, url.Values{"pnl_gt": {"-1"}, "limit": {"200"}})
	set := addrSet(wallets)
	if set[nullSnap] {
		t.Error("all-NULL snapshot must be excluded by pnl_gt")
	}
	if set[noSnap] {
		t.Error("wallet without snapshot must be excluded by pnl_gt")
	}

	// Even a <= 0 filter keeps them out (NULL never satisfies comparisons).
	wallets, _ = scan(t, f, url.Values{"pnl_lte": {"100000"}, "limit": {"200"}})
	set = addrSet(wallets)
	if set[nullSnap] || set[noSnap] {
		t.Error("NULL metrics must never match numeric filters")
	}

	// Without numeric filters the NULL rows are still listed (BR-07: shown
	// with nil metrics, not zeros).
	wallets, _ = scan(t, f, url.Values{"limit": {"200"}})
	for _, w := range wallets {
		if w.Address == nullSnap && w.Metrics != nil {
			t.Errorf("expected nil metrics for NULL snapshot, got %+v", w.Metrics)
		}
	}
}
