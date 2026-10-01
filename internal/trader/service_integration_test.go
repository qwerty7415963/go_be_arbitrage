//go:build integration

package trader

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
	"github.com/qwerty7415963/go_be_arbitrage/internal/tradergroup"
)

var testSecret = []byte("test-cursor-secret-32-bytes!!!")

func searchFixture(t *testing.T) (*Service, *Repository, *tradergroup.Repository, uuid.UUID, uuid.UUID, []string) {
	t.Helper()
	pool := testPool(t)
	t.Cleanup(pool.Close)
	repo := NewRepository(pool)
	groups := tradergroup.NewRepository(pool)
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
func TestService_Search_GroupFilter(t *testing.T) {
	svc, repo, groups, venueID, user, addrs := searchFixture(t)
	ctx := context.Background()

	g, err := groups.Create(ctx, user, "Alphas", "")
	if err != nil {
		t.Fatalf("group: %v", err)
	}
	t.Cleanup(func() {
		_, _ = repo.pool.Exec(ctx, `DELETE FROM trader_groups WHERE id = $1`, g.ID)
	})
	if _, err := groups.AddMembers(ctx, g.ID, user, []tradergroup.MemberInput{
		{Venue: testVenueCode, WalletAddress: addrs[0]},
		{Venue: testVenueCode, WalletAddress: addrs[1]},
	}); err != nil {
		t.Fatalf("members: %v", err)
	}

	req := &SearchRequest{Limit: 10, GroupID: g.ID.String(), Venue: testVenueCode}
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
