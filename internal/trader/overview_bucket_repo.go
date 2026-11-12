package trader

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

func pnlBucketCondition(bucketID string) (string, bool) {
	switch bucketID {
	case "extremely_profitable":
		return "realized_pnl >= 1000000", true
	case "very_profitable":
		return "realized_pnl > 100000 AND realized_pnl < 1000000", true
	case "profitable":
		return "realized_pnl > 0 AND realized_pnl <= 100000", true
	case "unprofitable":
		return "realized_pnl < 0 AND realized_pnl >= -100000", true
	case "very_unprofitable":
		return "realized_pnl < -100000 AND realized_pnl > -1000000", true
	case "rekt":
		return "realized_pnl <= -1000000", true
	}
	return "", false
}

func accountBucketCondition(bucketID string) (string, bool) {
	switch bucketID {
	case "apex":
		return "end_equity >= 5000000", true
	case "whale":
		return "end_equity >= 1000000 AND end_equity < 5000000", true
	case "large":
		return "end_equity >= 100000 AND end_equity < 1000000", true
	case "medium":
		return "end_equity >= 10000 AND end_equity < 100000", true
	case "small":
		return "end_equity > 0 AND end_equity < 10000", true
	}
	return "", false
}

func bucketMembershipCTE(kind, bucketID, period string) (string, bool) {
	switch kind {
	case "pnl":
		cond, ok := pnlBucketCondition(bucketID)
		if !ok {
			return "", false
		}
		return fmt.Sprintf(`WITH bucket_wallets AS (
			SELECT wallet_address FROM trader_period_metrics
			WHERE venue_id = $1 AND period = $2
			  AND realized_pnl IS NOT NULL AND realized_pnl <> 0
			  AND %s
		)`, cond), true
	case "account":
		cond, ok := accountBucketCondition(bucketID)
		if !ok {
			return "", false
		}
		return fmt.Sprintf(`WITH latest_equity AS (
			SELECT DISTINCT ON (wallet_address) wallet_address, end_equity
			FROM trader_equity_daily
			WHERE venue_id = $1 AND end_equity IS NOT NULL
			ORDER BY wallet_address, stat_date DESC
		),
		bucket_wallets AS (
			SELECT wallet_address FROM latest_equity
			WHERE end_equity > 0 AND %s
		)`, cond), true
	}
	return "", false
}

// BucketWalletAddresses returns wallets in a bucket.
func (r *Repository) BucketWalletAddresses(ctx context.Context, venueID uuid.UUID, kind, bucketID, period string) ([]string, error) {
	cte, ok := bucketMembershipCTE(kind, bucketID, period)
	if !ok {
		return nil, domain.NewError(domain.ErrCodeInvalidFilter, "invalid bucket")
	}
	q := cte + `
		SELECT wallet_address FROM bucket_wallets`
	var args []any
	if kind == "pnl" {
		args = []any{venueID, period}
	} else {
		args = []any{venueID}
	}
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// BucketPeriodAgg aggregates period metrics for wallets in a bucket.
func (r *Repository) BucketPeriodAgg(ctx context.Context, venueID uuid.UUID, period string, wallets []string) (int64, int64, float64, float64, float64, error) {
	var lc, sc int64
	var profit, loss, volume float64
	err := r.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(long_count), 0)::bigint,
		       COALESCE(SUM(short_count), 0)::bigint,
		       COALESCE(SUM(realized_pnl) FILTER (WHERE realized_pnl > 0), 0),
		       COALESCE(SUM(realized_pnl) FILTER (WHERE realized_pnl < 0), 0),
		       COALESCE(SUM(volume), 0)
		FROM trader_period_metrics
		WHERE venue_id = $1 AND period = $2 AND wallet_address = ANY($3)`,
		venueID, period, wallets).Scan(&lc, &sc, &profit, &loss, &volume)
	return lc, sc, profit, loss, volume, err
}

// BucketPositionNotional aggregates open positions for wallets.
func (r *Repository) BucketPositionNotional(ctx context.Context, venueID uuid.UUID, wallets []string) (float64, float64, int, error) {
	var longN, shortN float64
	var n int
	err := r.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(CASE WHEN side = 'LONG' THEN ABS(position_value) ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN side = 'SHORT' THEN ABS(position_value) ELSE 0 END), 0),
		       COUNT(DISTINCT wallet_address)::int
		FROM trader_positions
		WHERE venue_id = $1 AND wallet_address = ANY($2)`,
		venueID, wallets).Scan(&longN, &shortN, &n)
	return longN, shortN, n, err
}

// BucketCoverage counts wallets with positions/trades.
func (r *Repository) BucketCoverage(ctx context.Context, venueID uuid.UUID, wallets []string) (int, int, error) {
	var pos, trades int
	if err := r.pool.QueryRow(ctx, `
		SELECT COUNT(DISTINCT wallet_address)::int FROM trader_positions
		WHERE venue_id = $1 AND wallet_address = ANY($2)`, venueID, wallets).Scan(&pos); err != nil {
		return 0, 0, err
	}
	if err := r.pool.QueryRow(ctx, `
		SELECT COUNT(DISTINCT wallet_address)::int FROM trader_trades
		WHERE venue_id = $1 AND wallet_address = ANY($2)`, venueID, wallets).Scan(&trades); err != nil {
		return 0, 0, err
	}
	return pos, trades, nil
}

// BucketPositioning returns daily trade volume + daily stat counts for wallets.
type BucketPositioningDay struct {
	Date       time.Time
	TradeLong  float64
	TradeShort float64
	StatLong   int64
	StatShort  int64
}

func (r *Repository) BucketPositioning(ctx context.Context, venueID uuid.UUID, wallets []string, since time.Time) ([]BucketPositioningDay, error) {
	outMap := map[time.Time]*BucketPositioningDay{}
	get := func(d time.Time) *BucketPositioningDay {
		d = d.UTC().Truncate(24 * time.Hour)
		if v, ok := outMap[d]; ok {
			return v
		}
		v := &BucketPositioningDay{Date: d}
		outMap[d] = v
		return v
	}
	rows, err := r.pool.Query(ctx, `
		SELECT closed_at::date, side, COALESCE(SUM(volume), 0)
		FROM trader_trades
		WHERE venue_id = $1 AND wallet_address = ANY($2) AND closed_at >= $3
		GROUP BY closed_at::date, side`, venueID, wallets, since)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var d time.Time
		var side string
		var vol float64
		if err := rows.Scan(&d, &side, &vol); err != nil {
			rows.Close()
			return nil, err
		}
		day := get(d)
		if side == "LONG" {
			day.TradeLong += vol
		} else {
			day.TradeShort += vol
		}
	}
	rows.Close()
	rows2, err := r.pool.Query(ctx, `
		SELECT stat_date, COALESCE(SUM(long_count), 0)::bigint, COALESCE(SUM(short_count), 0)::bigint
		FROM trader_daily_stats
		WHERE venue_id = $1 AND wallet_address = ANY($2) AND stat_date >= $3
		GROUP BY stat_date`, venueID, wallets, since.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	defer rows2.Close()
	for rows2.Next() {
		var d time.Time
		var lc, sc int64
		if err := rows2.Scan(&d, &lc, &sc); err != nil {
			return nil, err
		}
		day := get(d)
		day.StatLong += lc
		day.StatShort += sc
	}
	out := make([]BucketPositioningDay, 0, len(outMap))
	for _, v := range outMap {
		out = append(out, *v)
	}
	return out, nil
}

// BucketPositioningSnapshots returns daily snapshot notional aggregates for wallets.
func (r *Repository) BucketPositioningSnapshots(ctx context.Context, venueID uuid.UUID, wallets []string, since time.Time) ([]BucketPositioningDay, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT snapshot_date, COALESCE(SUM(long_ntl), 0), COALESCE(SUM(short_ntl), 0)
		FROM trader_position_snapshot_daily
		WHERE venue_id = $1 AND wallet_address = ANY($2) AND snapshot_date >= $3
		GROUP BY snapshot_date
		ORDER BY snapshot_date`, venueID, wallets, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BucketPositioningDay
	for rows.Next() {
		var d time.Time
		var longN, shortN float64
		if err := rows.Scan(&d, &longN, &shortN); err != nil {
			return nil, err
		}
		out = append(out, BucketPositioningDay{
			Date:       d.UTC().Truncate(24 * time.Hour),
			TradeLong:  longN,
			TradeShort: shortN,
			StatLong:   0,
			StatShort:  0,
		})
	}
	return out, rows.Err()
}

// BucketWallets returns paginated wallets in a bucket.
type repoBucketWallet struct {
	Address       string
	DisplayName   *string
	PerpEquity    float64
	PositionValue float64
	Leverage      float64
	WinRate       float64
	VolumeRange   float64
	RealizedPnl   float64
	LongCount     int64
	ShortCount    int64
}

func (r *Repository) BucketWallets(ctx context.Context, venueID uuid.UUID, kind, bucketID, period string, rangeStart time.Time, sort, dir string, page, limit int) ([]repoBucketWallet, int, error) {
	cte, ok := bucketMembershipCTE(kind, bucketID, period)
	if !ok {
		return nil, 0, domain.NewError(domain.ErrCodeInvalidFilter, "invalid bucket")
	}
	// total
	var total int
	countQ := cte + ` SELECT COUNT(*) FROM bucket_wallets`
	var countArgs []any
	if kind == "pnl" {
		countArgs = []any{venueID, period}
	} else {
		countArgs = []any{venueID}
	}
	if err := r.pool.QueryRow(ctx, countQ, countArgs...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	offset := (page - 1) * limit

	sortExpr := "COALESCE(e.end_equity, 0)"
	switch sort {
	case "win_rate":
		sortExpr = "COALESCE(m.win_rate, 0)"
	case "volume":
		sortExpr = "COALESCE(tv.volume, m.volume, 0)"
	case "pnl":
		sortExpr = "COALESCE(m.realized_pnl, 0)"
	case "bias":
		sortExpr = "COALESCE(m.long_count, 0)::float / NULLIF(COALESCE(m.long_count, 0) + COALESCE(m.short_count, 0), 0)"
	}
	if dir != "asc" {
		dir = "desc"
	}

	q := cte + `,
		pos AS (
			SELECT venue_id, wallet_address,
			       COALESCE(SUM(ABS(position_value)), 0) AS pos_value,
			       COALESCE(AVG(leverage), 0) AS avg_leverage
			FROM trader_positions
			GROUP BY venue_id, wallet_address
		),
		tradevol AS (
			SELECT venue_id, wallet_address, COALESCE(SUM(volume), 0) AS volume
			FROM trader_trades
			WHERE closed_at >= $3
			GROUP BY venue_id, wallet_address
		)
		SELECT r.wallet_address, r.display_name,
		       COALESCE(e.end_equity, 0) AS perp_equity,
		       COALESCE(p.pos_value, 0) AS position_value,
		       CASE WHEN COALESCE(s.account_value, 0) <> 0
		            THEN COALESCE(s.total_ntl_pos, 0) / s.account_value
		            ELSE COALESCE(p.avg_leverage, 0) END AS leverage,
		       COALESCE(m.win_rate, 0) AS win_rate,
		       COALESCE(tv.volume, m.volume, 0) AS volume_range,
		       COALESCE(m.realized_pnl, 0) AS realized_pnl,
		       COALESCE(m.long_count, 0)::bigint AS long_count,
		       COALESCE(m.short_count, 0)::bigint AS short_count
		FROM bucket_wallets bw
		JOIN trader_registry r ON r.venue_id = $1 AND r.wallet_address = bw.wallet_address
		LEFT JOIN LATERAL (
			SELECT end_equity FROM trader_equity_daily
			WHERE venue_id = $1 AND wallet_address = bw.wallet_address AND end_equity IS NOT NULL
			ORDER BY stat_date DESC LIMIT 1
		) e ON true
		LEFT JOIN trader_period_metrics m
		  ON m.venue_id = $1 AND m.wallet_address = bw.wallet_address AND m.period = $2
		LEFT JOIN trader_position_summary s
		  ON s.venue_id = $1 AND s.wallet_address = bw.wallet_address
		LEFT JOIN pos p
		  ON p.venue_id = $1 AND p.wallet_address = bw.wallet_address
		LEFT JOIN tradevol tv
		  ON tv.venue_id = $1 AND tv.wallet_address = bw.wallet_address
		ORDER BY ` + sortExpr + ` ` + dir + `, r.wallet_address ASC
		LIMIT $4 OFFSET $5`
	rows, err := r.pool.Query(ctx, q, venueID, period, rangeStart, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []repoBucketWallet
	for rows.Next() {
		var w repoBucketWallet
		if err := rows.Scan(&w.Address, &w.DisplayName, &w.PerpEquity, &w.PositionValue,
			&w.Leverage, &w.WinRate, &w.VolumeRange, &w.RealizedPnl,
			&w.LongCount, &w.ShortCount); err != nil {
			return nil, 0, err
		}
		out = append(out, w)
	}
	return out, total, rows.Err()
}

// BucketMarkets returns paginated market aggregates for a bucket.
func (r *Repository) BucketMarkets(ctx context.Context, venueID uuid.UUID, wallets []string, rangeStart time.Time, search, sort, dir string, page, limit int) ([]BucketMarketRow, int, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	var posCount int
	if err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*)::int FROM trader_positions
		WHERE venue_id = $1 AND wallet_address = ANY($2)`, venueID, wallets).Scan(&posCount); err != nil {
		return nil, 0, err
	}
	if posCount > 0 {
		return r.bucketMarketsPositions(ctx, venueID, wallets, search, sort, dir, page, limit)
	}
	return r.bucketMarketsTrades(ctx, venueID, wallets, rangeStart, search, sort, dir, page, limit)
}

func (r *Repository) bucketMarketsPositions(ctx context.Context, venueID uuid.UUID, wallets []string, search, sort, dir string, page, limit int) ([]BucketMarketRow, int, error) {
	whereSearch := ""
	args := []any{venueID, wallets}
	if strings.TrimSpace(search) != "" {
		whereSearch = " AND coin ILIKE $" + fmt.Sprint(len(args)+1)
		args = append(args, "%"+search+"%")
	}
	// total distinct markets
	countQ := `SELECT COUNT(DISTINCT coin)::int FROM trader_positions WHERE venue_id = $1 AND wallet_address = ANY($2)` + whereSearch
	var total int
	if err := r.pool.QueryRow(ctx, countQ, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	sortExpr := "COALESCE(SUM(ABS(position_value)), 0)"
	switch sort {
	case "upnl":
		sortExpr = "COALESCE(SUM(unrealized_pnl), 0)"
	case "traders":
		sortExpr = "COUNT(DISTINCT wallet_address)"
	}
	if dir != "asc" {
		dir = "desc"
	}
	offset := (page - 1) * limit
	dataArgs := append([]any{}, args...)
	dataArgs = append(dataArgs, limit, offset)
	q := `SELECT coin,
		COALESCE(SUM(CASE WHEN side = 'LONG' THEN ABS(position_value) ELSE 0 END), 0) AS notional_long,
		COALESCE(SUM(CASE WHEN side = 'SHORT' THEN ABS(position_value) ELSE 0 END), 0) AS notional_short,
		COUNT(DISTINCT wallet_address) FILTER (WHERE side = 'LONG')::int AS traders_long,
		COUNT(DISTINCT wallet_address) FILTER (WHERE side = 'SHORT')::int AS traders_short,
		COALESCE(SUM(unrealized_pnl) FILTER (WHERE unrealized_pnl > 0), 0) AS pnl_profit,
		COALESCE(SUM(unrealized_pnl) FILTER (WHERE unrealized_pnl < 0), 0) AS pnl_loss
	FROM trader_positions
	WHERE venue_id = $1 AND wallet_address = ANY($2)` + whereSearch + `
	GROUP BY coin
	ORDER BY ` + sortExpr + ` ` + dir + `
	LIMIT $` + fmt.Sprint(len(dataArgs)-1) + ` OFFSET $` + fmt.Sprint(len(dataArgs))
	rows, err := r.pool.Query(ctx, q, dataArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []BucketMarketRow
	for rows.Next() {
		var m BucketMarketRow
		var market string
		if err := rows.Scan(&market, &m.NotionalLong, &m.NotionalShort, &m.TradersLong, &m.TradersShort, &m.PnlProfit, &m.PnlLoss); err != nil {
			return nil, 0, err
		}
		m.Market = market
		m.Sentiment = SentimentFloat(m.NotionalLong, m.NotionalShort)
		out = append(out, m)
	}
	return out, total, rows.Err()
}

func (r *Repository) bucketMarketsTrades(ctx context.Context, venueID uuid.UUID, wallets []string, rangeStart time.Time, search, sort, dir string, page, limit int) ([]BucketMarketRow, int, error) {
	args := []any{venueID, wallets, rangeStart}
	whereSearch := ""
	if strings.TrimSpace(search) != "" {
		whereSearch = " AND market ILIKE $" + fmt.Sprint(len(args)+1)
		args = append(args, "%"+search+"%")
	}
	countQ := `SELECT COUNT(DISTINCT market)::int FROM trader_trades WHERE venue_id = $1 AND wallet_address = ANY($2) AND closed_at >= $3` + whereSearch
	var total int
	if err := r.pool.QueryRow(ctx, countQ, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	sortExpr := "COALESCE(SUM(volume), 0)"
	switch sort {
	case "upnl":
		sortExpr = "COALESCE(SUM(pnl), 0)"
	case "traders":
		sortExpr = "COUNT(DISTINCT wallet_address)"
	}
	if dir != "asc" {
		dir = "desc"
	}
	offset := (page - 1) * limit
	dataArgs := append([]any{}, args...)
	dataArgs = append(dataArgs, limit, offset)
	q := `SELECT market,
		COALESCE(SUM(CASE WHEN side = 'LONG' THEN volume ELSE 0 END), 0) AS notional_long,
		COALESCE(SUM(CASE WHEN side = 'SHORT' THEN volume ELSE 0 END), 0) AS notional_short,
		COUNT(DISTINCT wallet_address) FILTER (WHERE side = 'LONG')::int AS traders_long,
		COUNT(DISTINCT wallet_address) FILTER (WHERE side = 'SHORT')::int AS traders_short,
		COALESCE(SUM(pnl) FILTER (WHERE pnl > 0), 0) AS pnl_profit,
		COALESCE(SUM(pnl) FILTER (WHERE pnl < 0), 0) AS pnl_loss
	FROM trader_trades
	WHERE venue_id = $1 AND wallet_address = ANY($2) AND closed_at >= $3` + whereSearch + `
	GROUP BY market
	ORDER BY ` + sortExpr + ` ` + dir + `
	LIMIT $` + fmt.Sprint(len(dataArgs)-1) + ` OFFSET $` + fmt.Sprint(len(dataArgs))
	rows, err := r.pool.Query(ctx, q, dataArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []BucketMarketRow
	for rows.Next() {
		var m BucketMarketRow
		var market string
		if err := rows.Scan(&market, &m.NotionalLong, &m.NotionalShort, &m.TradersLong, &m.TradersShort, &m.PnlProfit, &m.PnlLoss); err != nil {
			return nil, 0, err
		}
		m.Market = market
		m.Sentiment = SentimentFloat(m.NotionalLong, m.NotionalShort)
		out = append(out, m)
	}
	return out, total, rows.Err()
}

// UpsertPositionSnapshotDaily upserts a daily position snapshot row.
func (r *Repository) UpsertPositionSnapshotDaily(ctx context.Context, venueID uuid.UUID, addr string, snapshotDate time.Time, longNtl, shortNtl float64, accountValue, upnl *float64, asOf time.Time) error {
	if asOf.IsZero() {
		asOf = time.Now().UTC()
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO trader_position_snapshot_daily
			(venue_id, wallet_address, snapshot_date, long_ntl, short_ntl, account_value, upnl, as_of, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NOW())
		ON CONFLICT (venue_id, wallet_address, snapshot_date) DO UPDATE SET
			long_ntl = EXCLUDED.long_ntl,
			short_ntl = EXCLUDED.short_ntl,
			account_value = EXCLUDED.account_value,
			upnl = EXCLUDED.upnl,
			as_of = EXCLUDED.as_of,
			updated_at = NOW()`,
		venueID, addr, snapshotDate.UTC().Truncate(24*time.Hour), longNtl, shortNtl, accountValue, upnl, asOf.UTC())
	return err
}
