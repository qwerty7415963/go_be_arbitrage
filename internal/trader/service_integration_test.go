//go:build integration

package trader

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

var testSecret = []byte("test-cursor-secret-32-bytes!!!")

func searchFixture(t *testing.T) (*Service, *Repository, *stubGroups, uuid.UUID, uuid.UUID, []string) {
	t.Helper()
	pool := testPool(t)
	t.Cleanup(pool.Close)
	repo := NewRepository(pool)
	groups := &stubGroups{owners: map[uuid.UUID]uuid.UUID{}}
	venueID := testVenue(t, pool)
	user := testUser(t, pool)
	svc := NewService(repo, groups, testSecret)

	addrs := make([]string, 5)
	for i := range addrs {
		addrs[i] = testAddr()
		if _, _, err := repo.UpsertRegistry(t.Context(), venueID, addrs[i],
			SourceLeaderboard, nil, nil); err != nil {
			t.Fatalf("registry: %v", err)
		}
	}
	t.Cleanup(func() {
		for _, a := range addrs {
			_, _ = pool.Exec(context.Background(),
				`DELETE FROM trader_registry WHERE venue_id = $1 AND wallet_address = $2`, venueID, a)
		}
	})

	// pnl:    5000 15000 25000 35000 nil
	// win:    40   55    60    70    nil
	// vol:    1k   5k    9k    20k   nil
	// trades: 4    6     8     10    nil
	pnls := []*float64{f64(5000), f64(15000), f64(25000), f64(35000), nil}
	wrs := []*float64{f64(40), f64(55), f64(60), f64(70), nil}
	vols := []*float64{f64(1000), f64(5000), f64(9000), f64(20000), nil}
	for i, addr := range addrs {
		tc := int64(4 + i*2)
		var tcp *int64
		if pnls[i] != nil {
			tcp = &tc
		}
		lt := time.Now().UTC().Add(-time.Duration(i) * time.Hour)
		if err := repo.UpsertPeriodMetrics(context.Background(), &PeriodMetrics{
			VenueID: venueID, WalletAddress: addr, Period: Period30D, AsOf: time.Now().UTC(),
			PnL: pnls[i], WinRate: wrs[i], Volume: vols[i], TradeCount: tcp,
			LastTradeAt: &lt, DataStatus: DataReady, CalculationVersion: 1,
		}); err != nil {
			t.Fatalf("period: %v", err)
		}
	}
	return svc, repo, groups, venueID, user, addrs
}

// stubGroups is a map-backed GroupOwner (avoids a trader→tradergroup import
// cycle in tests; real group semantics live in tradergroup's own suite).
type stubGroups struct {
	owners map[uuid.UUID]uuid.UUID
}

func (s *stubGroups) OwnerOf(_ context.Context, gid uuid.UUID) (uuid.UUID, error) {
	if o, ok := s.owners[gid]; ok {
		return o, nil
	}
	return uuid.Nil, domain.NewError(domain.ErrCodeNotFound, "group not found")
}

func testUser(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	tenantID, userID := uuid.New(), uuid.New()
	run := time.Now().UnixNano()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO tenants (id, name) VALUES ($1, $2)`,
		tenantID, fmt.Sprintf("tr-svc-%d", run)); err != nil {
		t.Fatalf("tenant: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, tenant_id, email, password_hash) VALUES ($1, $2, $3, 'x')`,
		userID, tenantID, fmt.Sprintf("tr-svc-%d@test.com", run)); err != nil {
		t.Fatalf("user: %v", err)
	}
	return userID
}

func mustInvalidFilter(t *testing.T, err error) {
	t.Helper()
	var appErr *domain.AppError
	if !errors.As(err, &appErr) || appErr.Code != domain.ErrCodeInvalidFilter {
		t.Fatalf("want INVALID_FILTER, got %v", err)
	}
}

// SRCH-H-01/02: AND filters + descending sort with stable order.
func TestService_Search_FilterSort(t *testing.T) {
	svc, _, _, _, user, _ := searchFixture(t)
	ctx := context.Background()

	req := &SearchRequest{}
	req.Normalize()
	req.Venue = testVenueCode
	req.PnLMin = f64(10000)
	req.WinRateMin = f64(50)
	res, err := svc.Search(ctx, user, req)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res.Rows) != 3 || res.HasMore {
		t.Fatalf("want 3 rows, got %d hasMore=%v", len(res.Rows), res.HasMore)
	}
	for i := 1; i < len(res.Rows); i++ {
		if *res.Rows[i-1].PnL < *res.Rows[i].PnL {
			t.Errorf("not desc: %+v", res.Rows)
		}
	}

	// Ascending flip (SRCH-H-02).
	req.SortDirection = "asc"
	res, err = svc.Search(ctx, user, req)
	if err != nil {
		t.Fatalf("search asc: %v", err)
	}
	for i := 1; i < len(res.Rows); i++ {
		if *res.Rows[i-1].PnL > *res.Rows[i].PnL {
			t.Errorf("not asc: %+v", res.Rows)
		}
	}
}

// SRCH-H-03: cursor pages cover all rows exactly once.
func TestService_Search_Pagination(t *testing.T) {
	svc, _, _, _, user, addrs := searchFixture(t)
	ctx := context.Background()

	seen := map[string]bool{}
	cursor := ""
	pages := 0
	for {
		req := &SearchRequest{Limit: 2, Cursor: cursor, Venue: testVenueCode}
		res, err := svc.Search(ctx, user, req)
		if err != nil {
			t.Fatalf("page: %v", err)
		}
		pages++
		for _, r := range res.Rows {
			if seen[r.WalletAddress] {
				t.Fatalf("duplicate row %s", r.WalletAddress)
			}
			seen[r.WalletAddress] = true
		}
		if !res.HasMore {
			if res.NextCursor != "" {
				t.Error("last page must not carry a cursor")
			}
			break
		}
		if res.NextCursor == "" {
			t.Fatal("has_more without cursor")
		}
		cursor = res.NextCursor
		if pages > 10 {
			t.Fatal("pagination did not terminate")
		}
	}
	if len(seen) != 5 {
		t.Errorf("want 5 distinct rows across pages, got %d", len(seen))
	}
	_ = addrs
}

// NULL-metric wallet appears in unfiltered scans (excluded by metric filters).
func TestService_Search_NullMetrics(t *testing.T) {
	svc, _, _, _, user, _ := searchFixture(t)
	ctx := context.Background()
	req := &SearchRequest{Limit: 10, Venue: testVenueCode}
	res, err := svc.Search(ctx, user, req)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	foundNull := false
	for _, r := range res.Rows {
		if r.PnL == nil {
			foundNull = true
		}
	}
	if !foundNull || len(res.Rows) != 5 {
		t.Errorf("unfiltered scan must include the NULL row: %d rows", len(res.Rows))
	}

	req.PnLMin = f64(0)
	res, err = svc.Search(ctx, user, req)
	if err != nil {
		t.Fatalf("filtered: %v", err)
	}
	for _, r := range res.Rows {
		if r.PnL == nil {
			t.Error("metric filter must exclude NULL rows")
		}
	}
}

// SRCH-H-04/05: invalid filter, unknown venue, tampered cursor.
func TestService_Search_Errors(t *testing.T) {
	svc, _, _, _, user, _ := searchFixture(t)
	ctx := context.Background()

	bad := &SearchRequest{}
	bad.Normalize()
	bad.ROIMin = f64(30)
	bad.ROIMax = f64(10)
	if _, err := svc.Search(ctx, user, bad); err == nil {
		t.Error("expected INVALID_FILTER")
	} else {
		mustInvalidFilter(t, err)
	}

	venue := &SearchRequest{Venue: "nope"}
	if _, err := svc.Search(ctx, user, venue); err == nil {
		t.Error("expected unknown venue error")
	} else {
		mustInvalidFilter(t, err)
	}

	cur := &SearchRequest{Limit: 2, Cursor: "forged.cursor"}
	if _, err := svc.Search(ctx, user, cur); err == nil {
		t.Error("expected cursor error")
	} else {
		mustInvalidFilter(t, err)
	}
}

// Group filter: members only; anonymous 401; foreign 403; unknown 404.
// Ownership comes from a stub; real group semantics are covered in the
// tradergroup suite (this also avoids a test import cycle).
func TestService_Search_GroupFilter(t *testing.T) {
	svc, repo, stub, venueID, user, addrs := searchFixture(t)
	ctx := context.Background()

	gid := uuid.New()
	if _, err := repo.pool.Exec(ctx, `INSERT INTO trader_groups (id, user_id, name) VALUES ($1, $2, 'Alphas')`,
		gid, user); err != nil {
		t.Fatalf("group: %v", err)
	}
	t.Cleanup(func() {
		_, _ = repo.pool.Exec(ctx, `DELETE FROM trader_groups WHERE id = $1`, gid)
	})
	for _, a := range []string{addrs[0], addrs[1]} {
		if _, err := repo.pool.Exec(ctx, `INSERT INTO trader_group_members
			(group_id, venue_id, wallet_address) VALUES ($1, $2, $3)`,
			gid, venueID, a); err != nil {
			t.Fatalf("member: %v", err)
		}
	}
	stub.owners[gid] = user

	req := &SearchRequest{Limit: 10, GroupID: gid.String(), Venue: testVenueCode}
	res, err := svc.Search(ctx, user, req)
	if err != nil {
		t.Fatalf("group search: %v", err)
	}
	if len(res.Rows) != 2 {
		t.Fatalf("want 2 members, got %d", len(res.Rows))
	}

	if _, err := svc.Search(ctx, uuid.Nil, req); err == nil {
		t.Error("anonymous group filter must fail")
	}
	other := uuid.New()
	if _, err := svc.Search(ctx, other, req); err == nil {
		t.Error("foreign group must fail")
	} else {
		var appErr *domain.AppError
		if !errors.As(err, &appErr) || appErr.Code != domain.ErrCodeGroupForbidden {
			t.Errorf("want GROUP-003, got %v", err)
		}
	}
	req.GroupID = uuid.NewString()
	if _, err := svc.Search(ctx, user, req); err == nil {
		t.Error("unknown group must fail")
	}
	_ = venueID
}

// TRD-H-01/02: detail header + metrics; unknown 404; bad address 400;
// sync-state created on demand (BE-038).
func TestService_Detail(t *testing.T) {
	svc, repo, _, venueID, _, addrs := searchFixture(t)
	ctx := context.Background()

	d, err := svc.Detail(ctx, testVenueCode, addrs[0], "30D")
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if d.Registry == nil || d.Registry.WalletAddress != addrs[0] {
		t.Errorf("header: %+v", d.Registry)
	}
	if d.Metrics == nil || d.Metrics.PnL == nil || *d.Metrics.PnL != 5000 {
		t.Errorf("metrics: %+v", d.Metrics)
	}

	if _, err := svc.Detail(ctx, testVenueCode, testAddr(), "30D"); err == nil {
		t.Error("unknown wallet must 404")
	}
	if _, err := svc.Detail(ctx, testVenueCode, "zzz", "30D"); err == nil {
		t.Error("bad address must 400")
	}
	if _, err := svc.Detail(ctx, testVenueCode, addrs[0], "90D"); err == nil {
		t.Error("bad period must 400")
	}

	fresh := testAddr()
	if _, _, err := repo.UpsertRegistry(ctx, venueID, fresh, SourceLeaderboard, nil, nil); err != nil {
		t.Fatalf("registry: %v", err)
	}
	t.Cleanup(func() {
		_, _ = repo.pool.Exec(ctx,
			`DELETE FROM trader_registry WHERE venue_id = $1 AND wallet_address = $2`, venueID, fresh)
	})
	d2, err := svc.Detail(ctx, testVenueCode, fresh, "")
	if err != nil {
		t.Fatalf("fresh detail: %v", err)
	}
	if d2.Metrics != nil {
		t.Errorf("never-synced wallet must have null metrics: %+v", d2.Metrics)
	}
	if st, _ := repo.GetSyncState(ctx, venueID, fresh); st == nil {
		t.Error("detail must ensure sync state (on-demand priority)")
	}
}

func TestService_Search_LastTradeSort(t *testing.T) {
	svc, _, _, _, user, _ := searchFixture(t)
	ctx := context.Background()
	req := &SearchRequest{SortBy: "last_trade", SortDirection: "desc", Limit: 10, Venue: testVenueCode}
	res, err := svc.Search(ctx, user, req)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res.Rows) != 5 {
		t.Fatalf("want 5, got %d", len(res.Rows))
	}
	for i := 1; i < len(res.Rows); i++ {
		a, b := res.Rows[i-1].LastTradeAt, res.Rows[i].LastTradeAt
		if a != nil && b != nil && a.Before(*b) {
			t.Errorf("not desc by last_trade: %+v", res.Rows)
		}
	}
}

// pageReq builds an offset-path request for the shared fixture venue.
func pageReq(page, limit int) *SearchRequest {
	return &SearchRequest{Page: &page, Limit: limit, Venue: testVenueCode}
}

// PG-I-01/PG-I-07: offset walk pages 1→2→3 over the static 5-row fixture
// (limit 2, sort pnl desc): exact coverage with no dup/missing, global order
// stable across pages (wallet tiebreak), total/totalPages/has_more per page,
// and no cursor issued on the stateless path.
func TestService_Search_OffsetPages(t *testing.T) {
	svc, _, _, _, user, _ := searchFixture(t)
	ctx := context.Background()

	// Fixture pnl desc: 35000, 25000, 15000, 5000, NULL (NULLS LAST).
	wantOrder := []string{"35000", "25000", "15000", "5000", "<nil>"}
	var gotOrder []string
	seen := map[string]bool{}
	for _, tc := range []struct {
		page    int
		rows    int
		hasMore bool
	}{{1, 2, true}, {2, 2, true}, {3, 1, false}} {
		res, err := svc.Search(ctx, user, pageReq(tc.page, 2))
		if err != nil {
			t.Fatalf("page %d: %v", tc.page, err)
		}
		if res.Page != tc.page || res.Total != 5 || res.TotalPages != 3 {
			t.Errorf("page %d: want page/total=5/totalPages=3, got %+v", tc.page, res)
		}
		if res.HasMore != tc.hasMore {
			t.Errorf("page %d: want hasMore=%v, got %v", tc.page, tc.hasMore, res.HasMore)
		}
		if res.NextCursor != "" {
			t.Errorf("page %d: offset path must not issue a cursor", tc.page)
		}
		if len(res.Rows) != tc.rows {
			t.Fatalf("page %d: want %d rows, got %d", tc.page, tc.rows, len(res.Rows))
		}
		for _, r := range res.Rows {
			if seen[r.WalletAddress] {
				t.Fatalf("duplicate row %s across pages", r.WalletAddress)
			}
			seen[r.WalletAddress] = true
			if r.PnL == nil {
				gotOrder = append(gotOrder, "<nil>")
			} else {
				gotOrder = append(gotOrder, fmt.Sprintf("%v", *r.PnL))
			}
		}
	}
	if len(seen) != 5 {
		t.Errorf("want 5 distinct rows across pages, got %d", len(seen))
	}
	for i := range wantOrder {
		if gotOrder[i] != wantOrder[i] {
			t.Errorf("unstable order: want %v, got %v", wantOrder, gotOrder)
		}
	}
}

// PG-I-02: page > totalPages (total > 0) → empty data, has_more=false.
func TestService_Search_PageBeyondTotal(t *testing.T) {
	svc, _, _, _, user, _ := searchFixture(t)
	ctx := context.Background()
	res, err := svc.Search(ctx, user, pageReq(4, 2))
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res.Rows) != 0 || res.HasMore || res.NextCursor != "" {
		t.Errorf("beyond total: want empty + has_more=false, got %+v", res)
	}
	if res.Total != 5 || res.TotalPages != 3 || res.Page != 4 {
		t.Errorf("meta: want total=5 totalPages=3 page=4, got %+v", res)
	}
}

// PG-I-03: empty result set → total=0, totalPages=0, empty, has_more=false.
func TestService_Search_PageEmptyResult(t *testing.T) {
	svc, _, _, _, user, _ := searchFixture(t)
	ctx := context.Background()
	req := pageReq(1, 20)
	req.PnLMin = f64(1e12)
	res, err := svc.Search(ctx, user, req)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res.Rows) != 0 || res.Total != 0 || res.TotalPages != 0 || res.HasMore {
		t.Errorf("empty: got %+v", res)
	}
}

// PG-I-04: limit > remaining — first page holds everything, page 2 is empty.
func TestService_Search_PageLimitBeyondRemaining(t *testing.T) {
	svc, _, _, _, user, _ := searchFixture(t)
	ctx := context.Background()
	res, err := svc.Search(ctx, user, pageReq(1, 10))
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	if len(res.Rows) != 5 || res.TotalPages != 1 || res.HasMore {
		t.Errorf("page 1: want 5 rows totalPages=1 hasMore=false, got %+v", res)
	}
	res2, err := svc.Search(ctx, user, pageReq(2, 10))
	if err != nil {
		t.Fatalf("page 2: %v", err)
	}
	if len(res2.Rows) != 0 || res2.HasMore {
		t.Errorf("page 2: want empty, got %+v", res2)
	}
}

// PG-I-05: page + cursor in one body → offset wins even for a forged cursor.
func TestService_Search_PageIgnoresCursor(t *testing.T) {
	svc, _, _, _, user, _ := searchFixture(t)
	ctx := context.Background()
	req := pageReq(1, 2)
	req.Cursor = "forged.cursor"
	res, err := svc.Search(ctx, user, req)
	if err != nil {
		t.Fatalf("page must win over cursor (no INVALID_FILTER): %v", err)
	}
	if len(res.Rows) != 2 || res.Total != 5 || res.Page != 1 {
		t.Errorf("offset results: got %+v", res)
	}
}

// PG-I-06: page 0 / negative → INVALID_FILTER, no query executed.
func TestService_Search_PageInvalid(t *testing.T) {
	svc, _, _, _, user, _ := searchFixture(t)
	ctx := context.Background()
	for _, p := range []int{0, -1} {
		if _, err := svc.Search(ctx, user, pageReq(p, 20)); err == nil {
			t.Errorf("page %d: expected INVALID_FILTER", p)
		} else {
			mustInvalidFilter(t, err)
		}
	}
}

// PG-I-08: same page twice on static data → identical rows (stateless
// determinism, so an FE retry of a failed page is safe).
func TestService_Search_PageRepeatable(t *testing.T) {
	svc, _, _, _, user, _ := searchFixture(t)
	ctx := context.Background()
	first, err := svc.Search(ctx, user, pageReq(2, 2))
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := svc.Search(ctx, user, pageReq(2, 2))
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if len(first.Rows) != len(second.Rows) {
		t.Fatalf("row count changed: %d vs %d", len(first.Rows), len(second.Rows))
	}
	for i := range first.Rows {
		if first.Rows[i].WalletAddress != second.Rows[i].WalletAddress {
			t.Errorf("row %d changed: %s vs %s", i,
				first.Rows[i].WalletAddress, second.Rows[i].WalletAddress)
		}
	}
}

// PG-I-09 (B3 perf guard): COUNT(*) under realistic scanner filters must not
// seq-scan trader_period_metrics. Seeds 20k deterministic rows in a dedicated
// venue (hermetic vs live workers + other tests), ANALYZEs so the planner
// sees real stats, then EXPLAINs the exact count query the service runs.
// The raw plan + timing are logged for the BE report.
func TestService_Search_CountUsesIndex(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	t.Cleanup(pool.Close)
	repo := NewRepository(pool)

	const venueCode = "trader-pagecount-venue"
	if _, err := pool.Exec(ctx, `
		INSERT INTO venues (code, name, venue_type)
		VALUES ('trader-pagecount-venue', 'Trader Pagecount Venue', 'PERP_DEX')
		ON CONFLICT (code) DO NOTHING`); err != nil {
		t.Fatalf("venue: %v", err)
	}
	var venueID uuid.UUID
	if err := pool.QueryRow(ctx,
		`SELECT id FROM venues WHERE code = 'trader-pagecount-venue'`).Scan(&venueID); err != nil {
		t.Fatalf("venue id: %v", err)
	}

	const n = 20000
	addrs := make([]string, n)
	for i := range addrs {
		addrs[i] = fmt.Sprintf("0xcc%038x", i+1)
	}
	if _, err := pool.Exec(ctx,
		`DELETE FROM trader_registry WHERE venue_id = $1`, venueID); err != nil {
		t.Fatalf("pre-clean: %v", err)
	}
	if _, err := pool.CopyFrom(ctx,
		pgx.Identifier{"trader_registry"},
		[]string{"venue_id", "wallet_address", "discovery_source"},
		pgx.CopyFromSlice(len(addrs), func(i int) ([]any, error) {
			return []any{venueID, addrs[i], "leaderboard"}, nil
		})); err != nil {
		t.Fatalf("seed registry: %v", err)
	}
	now := time.Now().UTC()
	if _, err := pool.CopyFrom(ctx,
		pgx.Identifier{"trader_period_metrics"},
		[]string{"venue_id", "wallet_address", "period", "as_of", "pnl", "win_rate",
			"volume", "trade_count", "data_status", "calculation_version"},
		pgx.CopyFromSlice(n, func(i int) ([]any, error) {
			// Deterministic spread; every 10th row keeps NULL metrics.
			var pnl, wr, vol any
			var tc any
			if i%10 != 0 {
				p := float64((i*7919)%40000) - 5000
				w := float64((i*104729)%10000) / 100
				v := float64((i*1299709)%1000000) + 100
				c := int64((i*31)%500) + 1
				pnl, wr, vol, tc = p, w, v, c
			}
			return []any{venueID, addrs[i], Period30D, now, pnl, wr, vol, tc, "ready", 1}, nil
		})); err != nil {
		t.Fatalf("seed periods: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM trader_registry WHERE venue_id = $1`, venueID)
	})
	if _, err := pool.Exec(ctx, `ANALYZE trader_period_metrics`); err != nil {
		t.Fatalf("analyze: %v", err)
	}

	req := &SearchRequest{Venue: venueCode, Period: Period30D, SortBy: "pnl",
		SortDirection: "desc", Limit: 20}
	req.Normalize()
	req.PnLMin = f64(0)
	cq, cargs := buildSearchCountQuery(req, venueID, nil)

	explain := func(name, q string, args []any) {
		t.Helper()
		var planJSON string
		start := time.Now()
		if err := pool.QueryRow(ctx, `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) `+q, args...).Scan(&planJSON); err != nil {
			t.Fatalf("explain %s: %v", name, err)
		}
		t.Logf("%s plan (%s, %d rows venue): %s", name, time.Since(start), n, compactPlan(t, planJSON))
		if hasSeqScan(t, planJSON, "trader_period_metrics") {
			t.Errorf("%s seq-scans trader_period_metrics (plan above)", name)
		}
	}
	explain("COUNT pnl-filter", cq, cargs)

	// Offset data query under the same filters (page 3 window).
	dq, dargs := buildSearchOffsetQuery(req, venueID, nil, 40)
	explain("DATA pnl-filter offset=40", dq, dargs)

	// Second realistic filter set on another sort metric (win_rate + volume).
	req2 := &SearchRequest{Venue: venueCode, Period: Period30D, SortBy: "win_rate",
		SortDirection: "desc", Limit: 20}
	req2.Normalize()
	req2.WinRateMin = f64(50)
	req2.VolumeMin = f64(100000)
	cq2, cargs2 := buildSearchCountQuery(req2, venueID, nil)
	explain("COUNT wr+vol-filter", cq2, cargs2)

	// Same filters through the service: total matches the seed, pages add up.
	svc := NewService(repo, &stubGroups{owners: map[uuid.UUID]uuid.UUID{}}, testSecret)
	user := testUser(t, pool)
	got := int64(0)
	pages := 0
	for p := 1; ; p++ {
		r := &SearchRequest{Venue: venueCode, Period: Period30D, SortBy: "pnl",
			SortDirection: "desc", Limit: 100, Page: &p}
		r.Normalize()
		r.PnLMin = f64(0)
		res, err := svc.Search(ctx, user, r)
		if err != nil {
			t.Fatalf("page %d: %v", p, err)
		}
		if p == 1 {
			got = res.Total
			if want := int((got + 99) / 100); res.TotalPages != want {
				t.Errorf("totalPages: want %d, got %d", want, res.TotalPages)
			}
		} else if res.Total != got {
			t.Errorf("total changed across pages: %d vs %d", got, res.Total)
		}
		pages++
		if !res.HasMore {
			break
		}
		if pages > 1000 {
			t.Fatal("offset walk did not terminate")
		}
	}
	t.Logf("offset walk: total=%d pages=%d", got, pages)
}

// walkPlan walks a decoded EXPLAIN (FORMAT JSON) tree, which nests
// map[string]any and []any arbitrarily under the top-level "Plan" object.
func walkPlan(n any, visit func(node map[string]any)) {
	switch v := n.(type) {
	case map[string]any:
		visit(v)
		for _, c := range v {
			walkPlan(c, visit)
		}
	case []any:
		for _, c := range v {
			walkPlan(c, visit)
		}
	}
}

// decodePlan decodes EXPLAIN (FORMAT JSON) output into a generic tree.
func decodePlan(t *testing.T, planJSON string) any {
	t.Helper()
	var tree any
	if err := json.Unmarshal([]byte(planJSON), &tree); err != nil {
		t.Fatalf("plan decode: %v", err)
	}
	return tree
}

// hasSeqScan reports whether the JSON plan contains a Seq Scan on rel.
func hasSeqScan(t *testing.T, planJSON, rel string) bool {
	t.Helper()
	found := false
	walkPlan(decodePlan(t, planJSON), func(node map[string]any) {
		if node["Node Type"] == "Seq Scan" && node["Relation Name"] == rel {
			found = true
		}
	})
	return found
}

// compactPlan extracts node types + timing for readable logs.
func compactPlan(t *testing.T, planJSON string) string {
	t.Helper()
	var out []string
	walkPlan(decodePlan(t, planJSON), func(v map[string]any) {
		if nt, ok := v["Node Type"].(string); ok {
			rel, _ := v["Relation Name"].(string)
			idx, _ := v["Index Name"].(string)
			out = append(out, fmt.Sprintf("%s %s %s (cost=%.0f rows=%.0f time=%.3f..%.3f)",
				nt, rel, idx, num(v["Total Cost"]), num(v["Plan Rows"]),
				num(v["Actual Startup Time"]), num(v["Actual Total Time"])))
		}
	})
	return strings.Join(out, " -> ")
}

func num(v any) float64 {
	if f, ok := v.(float64); ok {
		return f
	}
	return 0
}
