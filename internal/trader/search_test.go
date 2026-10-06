package trader

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func mustContain(t *testing.T, q, frag string) {
	t.Helper()
	if !strings.Contains(q, frag) {
		t.Errorf("query missing %q in:\n%s", frag, q)
	}
}

// SRCH builder: filters, sort, keyset cursor and group join compile to the
// expected predicates (unit level, no DB).
func TestBuildSearchQuery(t *testing.T) {
	venue := uuid.New()
	req := &SearchRequest{}
	req.Normalize()
	req.PnLMin = f64(10000)
	req.WinRateMin = f64(60)
	req.SortBy = "pnl"
	req.SortDirection = "desc"
	req.Limit = 10

	q, args := buildSearchQuery(req, venue, nil, nil, "")
	mustContain(t, q, "p.venue_id = $1")
	mustContain(t, q, "p.period = $2")
	mustContain(t, q, "p.pnl IS NOT NULL")
	mustContain(t, q, "p.win_rate IS NOT NULL")
	mustContain(t, q, "ORDER BY p.pnl DESC NULLS LAST, p.wallet_address ASC")
	mustContain(t, q, "LIMIT $")
	if len(args) != 5 { // venue, period, win_rate_min, pnl_min, limit
		t.Errorf("args: %v", args)
	}

	// Ascending sort flips the keyset operator; NULL cursor seeks NULL tail.
	req.SortDirection = "asc"
	m := "5"
	q2, _ := buildSearchQuery(req, venue, nil, &m, "0xabc")
	mustContain(t, q2, "ORDER BY p.pnl ASC NULLS LAST")
	mustContain(t, q2, "p.pnl > $")
	mustContain(t, q2, "p.pnl IS NULL")
	mustContain(t, q2, "p.wallet_address > $")

	q3, _ := buildSearchQuery(req, venue, nil, nil, "0xabc")
	mustContain(t, q3, "p.pnl IS NULL AND p.wallet_address > $")

	// Group join scopes rows to memberships.
	gid := uuid.New()
	q4, _ := buildSearchQuery(req, venue, &gid, nil, "")
	mustContain(t, q4, "JOIN trader_group_members gm")
	mustContain(t, q4, "gm.group_id = $")

	// Long/short win rates filter via derived expression.
	req.LongWinRateMin = f64(50)
	q5, _ := buildSearchQuery(req, venue, nil, nil, "")
	mustContain(t, q5, "100.0 * p.long_wins / p.long_count")
}

// PG-U-06: offset query — identical filters/stable ORDER as the keyset path,
// exact LIMIT + OFFSET, no cursor predicates.
func TestBuildSearchOffsetQuery(t *testing.T) {
	venue := uuid.New()
	req := &SearchRequest{}
	req.Normalize()
	req.PnLMin = f64(10000)
	req.WinRateMin = f64(60)
	req.SortBy = "pnl"
	req.SortDirection = "desc"
	req.Limit = 10

	q, args := buildSearchOffsetQuery(req, venue, nil, 20)
	mustContain(t, q, "p.venue_id = $1")
	mustContain(t, q, "p.period = $2")
	mustContain(t, q, "p.pnl IS NOT NULL")
	mustContain(t, q, "p.win_rate IS NOT NULL")
	mustContain(t, q, "ORDER BY p.pnl DESC NULLS LAST, p.wallet_address ASC")
	mustContain(t, q, "LIMIT $")
	mustContain(t, q, "OFFSET $")
	if strings.Contains(q, "p.pnl < $") || strings.Contains(q, "p.pnl > $") {
		t.Errorf("offset query must not carry keyset predicates:\n%s", q)
	}
	// venue, period, win_rate_min, pnl_min, limit, offset.
	if len(args) != 6 {
		t.Errorf("args: %v", args)
	}
	if args[4] != 10 || args[5] != 20 {
		t.Errorf("want LIMIT 10 OFFSET 20, got %v", args[4:])
	}

	// Ascending sort flips ORDER but keeps the shape.
	req.SortDirection = "asc"
	q2, _ := buildSearchOffsetQuery(req, venue, nil, 0)
	mustContain(t, q2, "ORDER BY p.pnl ASC NULLS LAST, p.wallet_address ASC")

	// Group join scopes rows identically to the keyset path.
	gid := uuid.New()
	q3, _ := buildSearchOffsetQuery(req, venue, &gid, 0)
	mustContain(t, q3, "JOIN trader_group_members gm")
}

// PG-U-07: COUNT query — SELECT COUNT(*) over the IDENTICAL WHERE as the
// offset data query (same filters, no ORDER/LIMIT/OFFSET/cursor).
func TestBuildSearchCountQuery(t *testing.T) {
	venue := uuid.New()
	req := &SearchRequest{}
	req.Normalize()
	req.PnLMin = f64(10000)
	req.WinRateMin = f64(60)
	req.SortBy = "pnl"
	req.SortDirection = "desc"
	req.Limit = 10

	cq, cargs := buildSearchCountQuery(req, venue, nil)
	mustContain(t, cq, "SELECT COUNT(*)")
	mustContain(t, cq, "p.venue_id = $1")
	mustContain(t, cq, "p.pnl IS NOT NULL")
	if strings.Contains(cq, "ORDER BY") || strings.Contains(cq, "LIMIT") || strings.Contains(cq, "OFFSET") {
		t.Errorf("count query must have no ORDER/LIMIT/OFFSET:\n%s", cq)
	}
	// venue, period, win_rate_min, pnl_min — no limit/offset/cursor params.
	if len(cargs) != 4 {
		t.Errorf("args: %v", cargs)
	}

	dq, _ := buildSearchOffsetQuery(req, venue, nil, 0)
	if whereOf(t, cq) != whereOf(t, dq) {
		t.Errorf("COUNT WHERE differs from data WHERE:\ncount: %s\ndata:  %s", whereOf(t, cq), whereOf(t, dq))
	}

	// Group join present in both.
	gid := uuid.New()
	gc, _ := buildSearchCountQuery(req, venue, &gid)
	mustContain(t, gc, "JOIN trader_group_members gm")
	gd, _ := buildSearchOffsetQuery(req, venue, &gid, 0)
	if whereOf(t, gc) != whereOf(t, gd) {
		t.Errorf("grouped COUNT WHERE differs from data WHERE:\n%s\n%s", gc, gd)
	}
}

// whereOf extracts the WHERE clause (up to ORDER BY or end of string) so
// tests can assert filter identity across query shapes.
func whereOf(t *testing.T, q string) string {
	t.Helper()
	i := strings.Index(q, "WHERE ")
	if i < 0 {
		t.Fatalf("no WHERE in:\n%s", q)
	}
	w := q[i+len("WHERE "):]
	if j := strings.Index(w, " ORDER BY"); j >= 0 {
		w = w[:j]
	}
	return strings.TrimSpace(w)
}
