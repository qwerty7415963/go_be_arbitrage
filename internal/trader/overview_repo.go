package trader

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// PnLBucketAgg is one aggregated PnL bucket row.
type PnLBucketAgg struct {
	Bucket     string
	Count      int
	PnlSum     float64
	VolumeSum  float64
	LongCount  int64
	ShortCount int64
}

// AccountBucketAgg is one aggregated account bucket row.
type AccountBucketAgg struct {
	Bucket     string
	Count      int
	EquitySum  float64
	VolumeSum  float64
	LongCount  int64
	ShortCount int64
}

// DailyPnLSeriesRow is one (date, bucket, sum) row.
type DailyPnLSeriesRow struct {
	Date   time.Time
	Bucket string
	Pnl    float64
}

func pnlColName(pnlCol string) string {
	if pnlCol == "realized_pnl" {
		return "realized_pnl"
	}
	return "pnl"
}

// AggregatePnLBuckets aggregates trader_period_metrics into PnL buckets.
func (r *Repository) AggregatePnLBuckets(ctx context.Context, venueID uuid.UUID, period, pnlCol string) ([]PnLBucketAgg, error) {
	col := pnlColName(pnlCol)
	q := `
		SELECT
		  CASE
		    WHEN ` + col + ` >= 1000000 THEN 'extremely_profitable'
		    WHEN ` + col + ` > 100000 THEN 'very_profitable'
		    WHEN ` + col + ` > 0 AND ` + col + ` <= 100000 THEN 'profitable'
		    WHEN ` + col + ` < 0 AND ` + col + ` >= -100000 THEN 'unprofitable'
		    WHEN ` + col + ` > -1000000 THEN 'very_unprofitable'
		    ELSE 'rekt'
		  END AS bucket,
		  COUNT(*)::int AS wallet_count,
		  COALESCE(SUM(` + col + `), 0) AS pnl_total,
		  COALESCE(SUM(volume), 0) AS volume_total,
		  COALESCE(SUM(long_count), 0)::bigint AS long_count,
		  COALESCE(SUM(short_count), 0)::bigint AS short_count
		FROM trader_period_metrics
		WHERE venue_id = $1 AND period = $2 AND ` + col + ` IS NOT NULL AND ` + col + ` <> 0
		GROUP BY 1`
	rows, err := r.pool.Query(ctx, q, venueID, period)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PnLBucketAgg
	for rows.Next() {
		var a PnLBucketAgg
		if err := rows.Scan(&a.Bucket, &a.Count, &a.PnlSum, &a.VolumeSum, &a.LongCount, &a.ShortCount); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// PnlWalletCount counts wallets with non-null pnl metric.
func (r *Repository) PnlWalletCount(ctx context.Context, venueID uuid.UUID, period, pnlCol string) (int, error) {
	col := pnlColName(pnlCol)
	var n int
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*)::int FROM trader_period_metrics WHERE venue_id=$1 AND period=$2 AND `+col+` IS NOT NULL AND `+col+` <> 0`,
		venueID, period).Scan(&n)
	return n, err
}

// UniverseSize counts all period metric rows.
func (r *Repository) UniverseSize(ctx context.Context, venueID uuid.UUID, period string) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*)::int FROM trader_period_metrics WHERE venue_id=$1 AND period=$2`,
		venueID, period).Scan(&n)
	return n, err
}

// AggregateAccountBuckets aggregates latest equity per wallet into account buckets.
func (r *Repository) AggregateAccountBuckets(ctx context.Context, venueID uuid.UUID, period string) ([]AccountBucketAgg, error) {
	q := `
		WITH latest_equity AS (
		  SELECT DISTINCT ON (wallet_address) wallet_address, end_equity
		  FROM trader_equity_daily
		  WHERE venue_id = $1 AND end_equity IS NOT NULL
		  ORDER BY wallet_address, stat_date DESC
		),
		bucketed AS (
		  SELECT
		    CASE
		      WHEN end_equity >= 5000000 THEN 'apex'
		      WHEN end_equity >= 1000000 THEN 'whale'
		      WHEN end_equity >= 100000 THEN 'large'
		      WHEN end_equity >= 10000 THEN 'medium'
		      ELSE 'small'
		    END AS bucket,
		    end_equity,
		    wallet_address
		  FROM latest_equity
		  WHERE end_equity > 0
		)
		SELECT b.bucket,
		  COUNT(*)::int AS wallet_count,
		  COALESCE(SUM(b.end_equity), 0) AS equity_total,
		  COALESCE(SUM(p.volume), 0) AS volume_total,
		  COALESCE(SUM(p.long_count), 0)::bigint AS long_count,
		  COALESCE(SUM(p.short_count), 0)::bigint AS short_count
		FROM bucketed b
		LEFT JOIN trader_period_metrics p
		  ON p.venue_id = $1 AND p.wallet_address = b.wallet_address AND p.period = $2
		GROUP BY b.bucket`
	rows, err := r.pool.Query(ctx, q, venueID, period)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AccountBucketAgg
	for rows.Next() {
		var a AccountBucketAgg
		if err := rows.Scan(&a.Bucket, &a.Count, &a.EquitySum, &a.VolumeSum, &a.LongCount, &a.ShortCount); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// AccountWalletCount counts distinct wallets with latest equity.
func (r *Repository) AccountWalletCount(ctx context.Context, venueID uuid.UUID) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(DISTINCT wallet_address)::int FROM (
		  SELECT DISTINCT ON (wallet_address) wallet_address
		  FROM trader_equity_daily
		  WHERE venue_id = $1 AND end_equity IS NOT NULL AND end_equity > 0
		  ORDER BY wallet_address, stat_date DESC
		) t`, venueID).Scan(&n)
	return n, err
}

// AggregateDailyPnLSeries aggregates daily realized pnl per bucket over the window.
func (r *Repository) AggregateDailyPnLSeries(ctx context.Context, venueID uuid.UUID, period, pnlCol string, since time.Time) ([]DailyPnLSeriesRow, error) {
	col := pnlColName(pnlCol)
	q := `
		WITH wallet_bucket AS (
		  SELECT wallet_address,
		    CASE
		      WHEN ` + col + ` >= 1000000 THEN 'extremely_profitable'
		      WHEN ` + col + ` > 100000 THEN 'very_profitable'
		      WHEN ` + col + ` > 0 AND ` + col + ` <= 100000 THEN 'profitable'
		      WHEN ` + col + ` < 0 AND ` + col + ` >= -100000 THEN 'unprofitable'
		      WHEN ` + col + ` > -1000000 THEN 'very_unprofitable'
		      ELSE 'rekt'
		    END AS bucket
		  FROM trader_period_metrics
		  WHERE venue_id = $1 AND period = $2 AND ` + col + ` IS NOT NULL AND ` + col + ` <> 0
		)
		SELECT d.stat_date, wb.bucket, COALESCE(SUM(d.realized_pnl), 0) AS pnl_sum
		FROM trader_daily_stats d
		JOIN wallet_bucket wb ON wb.wallet_address = d.wallet_address
		WHERE d.venue_id = $1 AND d.stat_date >= $3
		GROUP BY d.stat_date, wb.bucket
		ORDER BY d.stat_date, wb.bucket`
	rows, err := r.pool.Query(ctx, q, venueID, period, since.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DailyPnLSeriesRow
	for rows.Next() {
		var r DailyPnLSeriesRow
		if err := rows.Scan(&r.Date, &r.Bucket, &r.Pnl); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SeriesWalletCount counts distinct wallets with daily stats in window.
func (r *Repository) SeriesWalletCount(ctx context.Context, venueID uuid.UUID, period, pnlCol string, since time.Time) (int, error) {
	col := pnlColName(pnlCol)
	var n int
	err := r.pool.QueryRow(ctx, `
		WITH wallet_bucket AS (
		  SELECT wallet_address FROM trader_period_metrics
		  WHERE venue_id = $1 AND period = $2 AND `+col+` IS NOT NULL AND `+col+` <> 0
		)
		SELECT COUNT(DISTINCT d.wallet_address)::int
		FROM trader_daily_stats d
		JOIN wallet_bucket wb ON wb.wallet_address = d.wallet_address
		WHERE d.venue_id = $1 AND d.stat_date >= $3`, venueID, period, since.Format("2006-01-02")).Scan(&n)
	return n, err
}

// AggregateAccountDailyPnLSeries aggregates daily realized pnl per account bucket.
func (r *Repository) AggregateAccountDailyPnLSeries(ctx context.Context, venueID uuid.UUID, since time.Time) ([]DailyPnLSeriesRow, error) {
	q := `
		WITH latest_equity AS (
		  SELECT DISTINCT ON (wallet_address) wallet_address, end_equity
		  FROM trader_equity_daily
		  WHERE venue_id = $1 AND end_equity IS NOT NULL AND end_equity > 0
		  ORDER BY wallet_address, stat_date DESC
		),
		wallet_bucket AS (
		  SELECT wallet_address,
		    CASE
		      WHEN end_equity >= 5000000 THEN 'apex'
		      WHEN end_equity >= 1000000 THEN 'whale'
		      WHEN end_equity >= 100000 THEN 'large'
		      WHEN end_equity >= 10000 THEN 'medium'
		      ELSE 'small'
		    END AS bucket
		  FROM latest_equity
		)
		SELECT d.stat_date, wb.bucket, COALESCE(SUM(d.realized_pnl), 0) AS pnl_sum
		FROM trader_daily_stats d
		JOIN wallet_bucket wb ON wb.wallet_address = d.wallet_address
		WHERE d.venue_id = $1 AND d.stat_date >= $2
		GROUP BY d.stat_date, wb.bucket
		ORDER BY d.stat_date, wb.bucket`
	rows, err := r.pool.Query(ctx, q, venueID, since.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DailyPnLSeriesRow
	for rows.Next() {
		var r DailyPnLSeriesRow
		if err := rows.Scan(&r.Date, &r.Bucket, &r.Pnl); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// DataStatusCounts returns distribution of data_status.
func (r *Repository) DataStatusCounts(ctx context.Context, venueID uuid.UUID, period string) (map[string]int, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT data_status, COUNT(*)::int FROM trader_period_metrics
		WHERE venue_id = $1 AND period = $2 GROUP BY data_status`, venueID, period)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{"ready": 0, "stale": 0, "error": 0, "syncing": 0}
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			return nil, err
		}
		out[status] = n
	}
	return out, rows.Err()
}
