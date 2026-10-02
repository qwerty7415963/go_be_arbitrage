package trader

import (
	"context"
	"sort"
	"time"

	"github.com/google/uuid"
)

// EquityPoint is one account-value sample (mark-to-market: realized plus
// open-position value — the only source that sees unrealized performance).
type EquityPoint struct {
	Time  time.Time
	Value float64
}

// PortfolioFetcher is the venue equity seam: account-value histories keyed by
// upstream window name (day/week/month/allTime; perp* variants ignored).
type PortfolioFetcher interface {
	FetchPortfolio(ctx context.Context, address string) (map[string][]EquityPoint, error)
}

// EquityDay is one aggregated UTC day for trader_equity_daily. Net cash flow
// (deposits/withdrawals) is unknowable from account-value history alone, so
// it stays NULL and daily return is the simple configured formula
// end/start-1 (nil when start is 0); both conflate cash flows by design
// (spec fixture J documents the same limitation).
type EquityDay struct {
	Date        time.Time
	StartEquity *float64
	EndEquity   *float64
	PeakEquity  *float64
	DailyReturn *float64
}

// AggregateEquityDaily folds samples into per-UTC-day rows: start = first
// sample of the day, end = last, peak = max (spec BE-021).
func AggregateEquityDaily(points []EquityPoint) map[string]EquityDay {
	byDay := map[string][]EquityPoint{}
	for _, p := range points {
		key := p.Time.UTC().Truncate(24 * time.Hour).Format("2006-01-02")
		byDay[key] = append(byDay[key], p)
	}
	out := map[string]EquityDay{}
	for key, pts := range byDay {
		sort.SliceStable(pts, func(i, j int) bool { return pts[i].Time.Before(pts[j].Time) })
		day, _ := time.Parse("2006-01-02", key)
		d := EquityDay{Date: day.UTC()}
		peak := pts[0].Value
		for _, p := range pts {
			if p.Value > peak {
				peak = p.Value
			}
		}
		s, e := pts[0].Value, pts[len(pts)-1].Value
		d.StartEquity, d.EndEquity, d.PeakEquity = &s, &e, &peak
		if s != 0 {
			r := e/s - 1
			d.DailyReturn = &r
		}
		out[key] = d
	}
	return out
}

// MaxDrawdownPct computes peak-to-trough decline in percent over an ordered
// equity curve (spec BE-022: 100→120→90 = 25%). Nil when empty; 0 when the
// curve never declines.
func MaxDrawdownPct(values []float64) *float64 {
	if len(values) == 0 {
		return nil
	}
	peak := values[0]
	maxDD := 0.0
	for _, v := range values[1:] {
		if v > peak {
			peak = v
		}
		if peak > 0 {
			if dd := (peak - v) / peak * 100; dd > maxDD {
				maxDD = dd
			}
		}
	}
	return &maxDD
}

// UpsertEquityDaily replaces one equity day row (idempotent recompute).
func (r *Repository) UpsertEquityDaily(ctx context.Context, venueID uuid.UUID, addr string, day EquityDay) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO trader_equity_daily
			(venue_id, wallet_address, stat_date, start_equity, end_equity,
			 peak_equity, net_cash_flow, daily_return)
		VALUES ($1,$2,$3,$4,$5,$6,NULL,$7)
		ON CONFLICT (venue_id, wallet_address, stat_date) DO UPDATE SET
			start_equity = EXCLUDED.start_equity, end_equity = EXCLUDED.end_equity,
			peak_equity = EXCLUDED.peak_equity, daily_return = EXCLUDED.daily_return,
			updated_at = NOW()`,
		venueID, addr, day.Date.Format("2006-01-02"),
		day.StartEquity, day.EndEquity, day.PeakEquity, day.DailyReturn)
	return err
}

// ListEquityDaily returns end-equity values in [from, to] oldest-first for
// drawdown computation (nil entries for missing days are skipped by callers:
// drawdown runs on stored days only, documented).
func (r *Repository) ListEquityDaily(ctx context.Context, venueID uuid.UUID, addr string, from, to time.Time) ([]float64, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT end_equity FROM trader_equity_daily
		WHERE venue_id = $1 AND wallet_address = $2
		  AND stat_date >= $3 AND stat_date <= $4 AND end_equity IS NOT NULL
		ORDER BY stat_date ASC`,
		venueID, addr, from.Format("2006-01-02"), to.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []float64
	for rows.Next() {
		var v float64
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
