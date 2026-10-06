package trader

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// searchArgs carries bound parameters positionally ($1, $2, ...).
type searchArgs struct {
	vals []any
}

func (a *searchArgs) add(v any) string {
	a.vals = append(a.vals, v)
	return fmt.Sprintf("$%d", len(a.vals))
}

// metricColumn maps sort_by/filter keys to period_metrics columns.
func metricColumn(key string) string {
	if c, ok := sortableColumns[key]; ok {
		return c
	}
	return ""
}

// searchSelectCols is the single SELECT projection shared by the keyset and
// offset data queries (COUNT uses COUNT(*) over the same FROM/WHERE).
const searchSelectCols = `p.venue_id, v.code, p.wallet_address, r.display_name, p.period,
		p.as_of, p.pnl, p.realized_pnl, p.roi, p.win_rate, p.trade_count, p.volume, p.gross_profit,
		p.gross_loss, p.profit_factor, p.avg_trade_pnl, p.long_count, p.long_wins,
		p.short_count, p.short_wins, p.max_drawdown_pct, p.avg_holding_time_sec,
		p.last_trade_at, p.data_status, p.is_partial, p.calculation_version`

// searchFilter builds the FROM (+ venue/period/group JOINs) and WHERE
// (venue, period, every min/max filter) shared by ALL scanner reads: the
// keyset data query, the offset data query and the COUNT query. Sharing one
// function guarantees the COUNT sees identical filters (CONTRACT.md B2).
// Cursor predicates are NOT added here; each data builder adds its own.
func searchFilter(req *SearchRequest, venueID uuid.UUID, groupID *uuid.UUID, ac *searchArgs) (from string, where []string, col, dir string) {
	v := ac.add(venueID)
	p := ac.add(req.Period)

	from = `FROM trader_period_metrics p
	JOIN trader_registry r ON r.venue_id = p.venue_id AND r.wallet_address = p.wallet_address
	JOIN venues v ON v.id = p.venue_id`
	if groupID != nil {
		g := ac.add(*groupID)
		from += fmt.Sprintf(`
	JOIN trader_group_members gm ON gm.venue_id = p.venue_id
		AND gm.wallet_address = p.wallet_address AND gm.group_id = %s`, g)
	}
	where = []string{fmt.Sprintf("p.venue_id = %s", v), fmt.Sprintf("p.period = %s", p)}

	rangeF := func(col string, min, max *float64) {
		if min == nil && max == nil {
			return
		}
		where = append(where, fmt.Sprintf("p.%s IS NOT NULL", col))
		if min != nil {
			where = append(where, fmt.Sprintf("p.%s >= %s", col, ac.add(*min)))
		}
		if max != nil {
			where = append(where, fmt.Sprintf("p.%s <= %s", col, ac.add(*max)))
		}
	}
	rangeF("roi", req.ROIMin, req.ROIMax)
	rangeF("win_rate", req.WinRateMin, req.WinRateMax)
	rangeF("pnl", req.PnLMin, req.PnLMax)
	rangeF("volume", req.VolumeMin, req.VolumeMax)
	rangeF("profit_factor", req.ProfitFactorMin, req.ProfitFactorMax)
	// Long/short win rates derive from counts at read time (no stored column):
	// filter via expression 100.0*long_wins/NULLIF(long_count,0).
	winExpr := func(count, wins string) string {
		return fmt.Sprintf("(CASE WHEN p.%s IS NULL OR p.%s = 0 THEN NULL "+
			"ELSE 100.0 * p.%s / p.%s END)", count, count, wins, count)
	}
	for _, f := range []struct {
		expr string
		min  *float64
		max  *float64
	}{
		{winExpr("long_count", "long_wins"), req.LongWinRateMin, req.LongWinRateMax},
		{winExpr("short_count", "short_wins"), req.ShortWinRateMin, req.ShortWinRateMax},
	} {
		if f.min == nil && f.max == nil {
			continue
		}
		where = append(where, f.expr+" IS NOT NULL")
		if f.min != nil {
			where = append(where, f.expr+" >= "+ac.add(*f.min))
		}
		if f.max != nil {
			where = append(where, f.expr+" <= "+ac.add(*f.max))
		}
	}
	if req.TradeCountMin != nil || req.TradeCountMax != nil {
		where = append(where, "p.trade_count IS NOT NULL")
		if req.TradeCountMin != nil {
			where = append(where, fmt.Sprintf("p.trade_count >= %s", ac.add(*req.TradeCountMin)))
		}
		if req.TradeCountMax != nil {
			where = append(where, fmt.Sprintf("p.trade_count <= %s", ac.add(*req.TradeCountMax)))
		}
	}
	if req.LastTradeAfter != "" {
		where = append(where, fmt.Sprintf("p.last_trade_at IS NOT NULL AND p.last_trade_at >= %s::timestamptz",
			ac.add(req.LastTradeAfter)))
	}

	col = metricColumn(req.SortBy)
	dir = "DESC"
	if req.SortDirection == "asc" {
		dir = "ASC"
	}
	return from, where, col, dir
}

// orderBy renders the stable sort: metric DIR NULLS LAST + wallet tiebreak,
// so static datasets page without dup/missing on either path (CONTRACT B3).
func orderBy(col, dir string) string {
	return fmt.Sprintf("p.%s %s NULLS LAST, p.wallet_address ASC", col, dir)
}

// buildSearchQuery assembles the keyset-paginated scanner query (pure function,
// unit-tested). Ordering is always <metric> DIR NULLS LAST + address ASC for a
// stable snapshot per query (spec BE-025/BE-026). Behavior is unchanged by the
// numbered-pagination contract (CONTRACT.md 2026-10-06): same filters, same
// projection, same LIMIT+1 probing.
func buildSearchQuery(req *SearchRequest, venueID uuid.UUID, groupID *uuid.UUID, curVal *string, curAddr string) (string, []any) {
	ac := &searchArgs{}
	from, where, col, dir := searchFilter(req, venueID, groupID, ac)
	cast := "::numeric"
	if req.SortBy == "last_trade" {
		cast = "::timestamptz"
	}
	if curAddr != "" {
		a := ac.add(curAddr)
		if curVal == nil {
			// Cursor sits on NULLs: only later NULL rows qualify.
			where = append(where, fmt.Sprintf("p.%s IS NULL AND p.wallet_address > %s", col, a))
		} else {
			cv := ac.add(*curVal)
			op := ">"
			if dir == "DESC" {
				op = "<"
			}
			where = append(where, fmt.Sprintf(
				"(p.%s %s %s%s OR p.%s IS NULL OR (p.%s = %s%s AND p.wallet_address > %s))",
				col, op, cv, cast, col, col, cv, cast, a))
		}
	}

	limit := ac.add(req.Limit + 1)
	q := fmt.Sprintf(`SELECT %s
	%s WHERE %s ORDER BY %s LIMIT %s`,
		searchSelectCols, from, strings.Join(where, " AND "), orderBy(col, dir), limit)
	return q, ac.vals
}

// buildSearchOffsetQuery assembles the numbered-pagination scanner query
// (CONTRACT.md 2026-10-06, pure function, unit-tested): identical filters and
// stable ORDER as the keyset path, but exact LIMIT + OFFSET instead of the
// keyset predicate and LIMIT+1 probing. offset = (page-1)*limit.
func buildSearchOffsetQuery(req *SearchRequest, venueID uuid.UUID, groupID *uuid.UUID, offset int) (string, []any) {
	ac := &searchArgs{}
	from, where, col, dir := searchFilter(req, venueID, groupID, ac)
	limit := ac.add(req.Limit)
	off := ac.add(offset)
	q := fmt.Sprintf(`SELECT %s
	%s WHERE %s ORDER BY %s LIMIT %s OFFSET %s`,
		searchSelectCols, from, strings.Join(where, " AND "), orderBy(col, dir), limit, off)
	return q, ac.vals
}

// buildSearchCountQuery assembles SELECT COUNT(*) over the identical
// FROM/WHERE as the data queries (same searchFilter, no ORDER/LIMIT/cursor),
// yielding the total for total_pages = ceil(total/limit).
func buildSearchCountQuery(req *SearchRequest, venueID uuid.UUID, groupID *uuid.UUID) (string, []any) {
	ac := &searchArgs{}
	from, where, _, _ := searchFilter(req, venueID, groupID, ac)
	q := fmt.Sprintf(`SELECT COUNT(*) %s WHERE %s`, from, strings.Join(where, " AND "))
	return q, ac.vals
}
