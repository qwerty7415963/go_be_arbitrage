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
