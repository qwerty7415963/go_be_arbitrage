package trader

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

)

// GetBucketDetailPayload reads the precomputed bucket detail payload.
// Returns nil, nil when no row exists.
func (r *Repository) GetBucketDetailPayload(ctx context.Context, venueID uuid.UUID, kind, bucketID, period, rangeCode string) (*BucketDetail, error) {
	var payload []byte
	var refreshedAt time.Time
	err := r.pool.QueryRow(ctx, `
		SELECT payload, refreshed_at
		FROM trader_bucket_detail
		WHERE venue_id = $1 AND kind = $2 AND bucket_id = $3 AND period = $4 AND range_code = $5`,
		venueID, kind, bucketID, period, rangeCode).Scan(&payload, &refreshedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	var d BucketDetail
	if err := json.Unmarshal(payload, &d); err != nil {
		return nil, err
	}
	d.RefreshedAt = refreshedAt
	return &d, nil
}

// GetBucketMembers reads members from the read model.
func (r *Repository) GetBucketMembers(ctx context.Context, venueID uuid.UUID, kind, bucketID, period, sort, dir string, page, limit int) ([]repoBucketWallet, int, error) {
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

	sortExpr := "perp_equity"
	switch sort {
	case "win_rate":
		sortExpr = "win_rate"
	case "volume":
		sortExpr = "volume_range"
	case "pnl":
		sortExpr = "realized_pnl"
	case "bias":
		sortExpr = "COALESCE(long_count,0)::float / NULLIF(COALESCE(long_count,0)+COALESCE(short_count,0),0)"
	}
	if dir != "asc" {
		dir = "desc"
	}

	q := `
		SELECT wallet_address, display_name, perp_equity, position_value, sum_upnl,
		       leverage, win_rate, volume_range, realized_pnl, long_count, short_count
		FROM trader_bucket_members
		WHERE venue_id = $1 AND kind = $2 AND bucket_id = $3 AND period = $4
		ORDER BY ` + sortExpr + ` ` + dir + ` NULLS LAST, wallet_address ASC
		LIMIT $5 OFFSET $6`
	rows, err := r.pool.Query(ctx, q, venueID, kind, bucketID, period, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []repoBucketWallet
	for rows.Next() {
		var w repoBucketWallet
		var posValue sql.NullFloat64
		var sumUpnl sql.NullFloat64
		var winRate sql.NullFloat64
		var volumeRange sql.NullFloat64
		var realizedPnl sql.NullFloat64
		var displayName sql.NullString
		if err := rows.Scan(&w.Address, &displayName, &w.PerpEquity, &posValue, &sumUpnl,
			&w.Leverage, &winRate, &volumeRange, &realizedPnl,
			&w.LongCount, &w.ShortCount); err != nil {
			return nil, 0, err
		}
		if displayName.Valid {
			v := displayName.String
			w.DisplayName = &v
		}
		if posValue.Valid {
			v := posValue.Float64
			w.PositionValue = &v
		}
		if sumUpnl.Valid {
			v := sumUpnl.Float64
			w.SumUpnl = &v
		}
		if winRate.Valid {
			v := winRate.Float64
			w.WinRate = &v
		}
		if volumeRange.Valid {
			v := volumeRange.Float64
			w.VolumeRange = &v
		}
		if realizedPnl.Valid {
			v := realizedPnl.Float64
			w.RealizedPnl = &v
		}
		out = append(out, w)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	var total int
	err = r.pool.QueryRow(ctx, `
		SELECT wallet_count FROM trader_bucket_counts
		WHERE venue_id = $1 AND kind = $2 AND bucket_id = $3 AND period = $4`,
		venueID, kind, bucketID, period).Scan(&total)
	if err != nil {
		if err == pgx.ErrNoRows {
			err = r.pool.QueryRow(ctx, `
				SELECT COUNT(*)::int FROM trader_bucket_members
				WHERE venue_id = $1 AND kind = $2 AND bucket_id = $3 AND period = $4`,
				venueID, kind, bucketID, period).Scan(&total)
			if err != nil {
				return nil, 0, err
			}
		} else {
			return nil, 0, err
		}
	}
	return out, total, nil
}

// GetBucketMarkets reads markets from the read model.
func (r *Repository) GetBucketMarkets(ctx context.Context, venueID uuid.UUID, kind, bucketID, period, rangeCode, search, sort, dir string, page, limit int) ([]BucketMarketRow, int, error) {
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

	whereSearch := ""
	args := []any{venueID, kind, bucketID, period, rangeCode}
	if strings.TrimSpace(search) != "" {
		whereSearch = " AND market ILIKE $" + fmt.Sprint(len(args)+1)
		args = append(args, "%"+search+"%")
	}

	sortExpr := "(notional_long + notional_short)"
	switch sort {
	case "upnl":
		sortExpr = "(pnl_profit + pnl_loss)"
	case "traders":
		sortExpr = "(traders_long + traders_short)"
	}
	if dir != "asc" {
		dir = "desc"
	}

	q := `
		SELECT market, notional_long, notional_short, traders_long, traders_short,
		       pnl_profit, pnl_loss, sentiment, asset_id, asset_category, logo_url
		FROM trader_bucket_markets
		WHERE venue_id = $1 AND kind = $2 AND bucket_id = $3 AND period = $4 AND range_code = $5` + whereSearch + `
		ORDER BY ` + sortExpr + ` ` + dir + `
		LIMIT $` + fmt.Sprint(len(args)+1) + ` OFFSET $` + fmt.Sprint(len(args)+2)
	dataArgs := append([]any{}, args...)
	dataArgs = append(dataArgs, limit, offset)
	rows, err := r.pool.Query(ctx, q, dataArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []BucketMarketRow
	for rows.Next() {
		var m BucketMarketRow
		var market string
		var assetID *uuid.UUID
		var assetCategory sql.NullString
		var logoURL sql.NullString
		var sentiment sql.NullString
		if err := rows.Scan(&market, &m.NotionalLong, &m.NotionalShort, &m.TradersLong, &m.TradersShort,
			&m.PnlProfit, &m.PnlLoss, &sentiment, &assetID, &assetCategory, &logoURL); err != nil {
			return nil, 0, err
		}
		m.Market = market
		if sentiment.Valid {
			m.Sentiment = sentiment.String
		}
		if assetID != nil {
			m.AssetID = *assetID
		}
		if assetCategory.Valid {
			m.AssetCategory = assetCategory.String
		}
		if logoURL.Valid {
			m.LogoURL = logoURL.String
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	countQ := `SELECT COUNT(*)::int FROM trader_bucket_markets WHERE venue_id = $1 AND kind = $2 AND bucket_id = $3 AND period = $4 AND range_code = $5` + whereSearch
	var total int
	if err := r.pool.QueryRow(ctx, countQ, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if total == 0 {
		var c int
		err := r.pool.QueryRow(ctx, `SELECT markets_count FROM trader_bucket_counts WHERE venue_id=$1 AND kind=$2 AND bucket_id=$3 AND period=$4`,
			venueID, kind, bucketID, period).Scan(&c)
		if err == nil {
			total = c
		}
	}
	return out, total, nil
}

// UpsertBucketMembers replaces members for a bucket/period.
func (r *Repository) UpsertBucketMembers(ctx context.Context, venueID uuid.UUID, kind, bucketID, period string, rows []repoBucketWallet) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM trader_bucket_members WHERE venue_id=$1 AND kind=$2 AND bucket_id=$3 AND period=$4`,
		venueID, kind, bucketID, period); err != nil {
		return err
	}
	for _, w := range rows {
		if _, err := tx.Exec(ctx, `
			INSERT INTO trader_bucket_members
				(venue_id, kind, bucket_id, period, wallet_address, display_name, perp_equity,
				 position_value, sum_upnl, leverage, win_rate, volume_range, realized_pnl,
				 bias, cohort, long_count, short_count, refreshed_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,NOW())`,
			venueID, kind, bucketID, period, w.Address, w.DisplayName, w.PerpEquity,
			w.PositionValue, w.SumUpnl, w.Leverage, w.WinRate, w.VolumeRange, w.RealizedPnl,
			nil, nil, w.LongCount, w.ShortCount); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// UpsertBucketMarkets replaces markets for a bucket/period/range.
func (r *Repository) UpsertBucketMarkets(ctx context.Context, venueID uuid.UUID, kind, bucketID, period, rangeCode string, rows []BucketMarketRow) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM trader_bucket_markets WHERE venue_id=$1 AND kind=$2 AND bucket_id=$3 AND period=$4 AND range_code=$5`,
		venueID, kind, bucketID, period, rangeCode); err != nil {
		return err
	}
	for _, m := range rows {
		sentiment := m.Sentiment
		if sentiment == "" {
			sentiment = "neutral"
		}
		assetID := m.AssetID
		var assetPtr *uuid.UUID
		if assetID != uuid.Nil {
			assetPtr = &assetID
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO trader_bucket_markets
				(venue_id, kind, bucket_id, period, range_code, market, notional_long, notional_short,
				 traders_long, traders_short, pnl_profit, pnl_loss, sentiment, asset_id, asset_category, logo_url, refreshed_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,NOW())`,
			venueID, kind, bucketID, period, rangeCode, m.Market, m.NotionalLong, m.NotionalShort,
			m.TradersLong, m.TradersShort, m.PnlProfit, m.PnlLoss, sentiment, assetPtr, m.AssetCategory, m.LogoURL); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// UpsertBucketDetail upserts a detail payload.
func (r *Repository) UpsertBucketDetail(ctx context.Context, venueID uuid.UUID, kind, bucketID, period, rangeCode string, payload *BucketDetail, refreshedAt time.Time) error {
	if refreshedAt.IsZero() {
		refreshedAt = time.Now().UTC()
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `
		INSERT INTO trader_bucket_detail (venue_id, kind, bucket_id, period, range_code, payload, refreshed_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (venue_id, kind, bucket_id, period, range_code) DO UPDATE SET
			payload = EXCLUDED.payload,
			refreshed_at = EXCLUDED.refreshed_at`,
		venueID, kind, bucketID, period, rangeCode, b, refreshedAt)
	return err
}

// UpsertBucketCounts upserts counts.
func (r *Repository) UpsertBucketCounts(ctx context.Context, venueID uuid.UUID, kind, bucketID, period string, walletCount, marketsCount int) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO trader_bucket_counts (venue_id, kind, bucket_id, period, wallet_count, markets_count, refreshed_at)
		VALUES ($1,$2,$3,$4,$5,$6,NOW())
		ON CONFLICT (venue_id, kind, bucket_id, period) DO UPDATE SET
			wallet_count = EXCLUDED.wallet_count,
			markets_count = EXCLUDED.markets_count,
			refreshed_at = NOW()`,
		venueID, kind, bucketID, period, walletCount, marketsCount)
	return err
}

// GetProjectorState reads projector state.
func (r *Repository) GetProjectorState(ctx context.Context, venueID uuid.UUID) (*BucketProjectorState, error) {
	var st BucketProjectorState
	var lastErr sql.NullString
	err := r.pool.QueryRow(ctx, `
		SELECT venue_id, last_source_watermark, last_refreshed_at, refresh_duration_ms, refresh_status, last_error
		FROM trader_bucket_projector_state
		WHERE venue_id = $1`, venueID).Scan(
		&st.VenueID, &st.LastSourceWatermark, &st.LastRefreshedAt, &st.RefreshDurationMs, &st.RefreshStatus, &lastErr)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if lastErr.Valid {
		v := lastErr.String
		st.LastError = &v
	}
	return &st, nil
}

// UpsertProjectorState upserts projector state.
func (r *Repository) UpsertProjectorState(ctx context.Context, state *BucketProjectorState) error {
	var lastErr any
	if state.LastError != nil {
		lastErr = *state.LastError
	} else {
		lastErr = nil
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO trader_bucket_projector_state
			(venue_id, last_source_watermark, last_refreshed_at, refresh_duration_ms, refresh_status, last_error)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (venue_id) DO UPDATE SET
			last_source_watermark = EXCLUDED.last_source_watermark,
			last_refreshed_at = EXCLUDED.last_refreshed_at,
			refresh_duration_ms = EXCLUDED.refresh_duration_ms,
			refresh_status = EXCLUDED.refresh_status,
			last_error = EXCLUDED.last_error`,
		state.VenueID, state.LastSourceWatermark, state.LastRefreshedAt, state.RefreshDurationMs, state.RefreshStatus, lastErr)
	return err
}

// GetBucketMembershipWallets returns source wallets for a bucket.
func (r *Repository) GetBucketMembershipWallets(ctx context.Context, venueID uuid.UUID, kind, bucketID, period string) ([]string, error) {
	return r.BucketWalletAddresses(ctx, venueID, kind, bucketID, period)
}

// isBucketReadModelFresh helper on repository.
func (r *Repository) isBucketReadModelFresh(refreshedAt time.Time) bool {
	return time.Since(refreshedAt) < 240*time.Second
}

// Ensure pgxpool import used.

var _ = pgxpool.New

