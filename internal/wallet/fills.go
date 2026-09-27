package wallet

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// FillInput is a normalized venue fill ready for persistence and the
// metrics engine. RealizedPnl is net of fee.
type FillInput struct {
	PositionID     string
	Market         string
	ExchangeFillID string
	FilledAt       time.Time
	Side           string // LONG | SHORT
	Quantity       float64
	Price          float64
	RealizedPnl    float64
	Fee            float64
	Raw            []byte // original venue payload
}

// PersistedFill is a stored fill with its market (for per-market snapshots).
type PersistedFill struct {
	MetricFill
	Market string
}

// FillRepository persists normalized fills and writes metric snapshots.
type FillRepository struct {
	db *pgxpool.Pool
}

func NewFillRepository(db *pgxpool.Pool) *FillRepository {
	return &FillRepository{db: db}
}

// UpsertFills inserts fills idempotently: re-delivery of the same venue fill
// is a no-op (ING-U-05). Returns rows actually inserted.
func (r *FillRepository) UpsertFills(ctx context.Context, walletID, venueID uuid.UUID, fills []FillInput) (int64, error) {
	if len(fills) == 0 {
		return 0, nil
	}
	batch := &pgx.Batch{}
	for _, f := range fills {
		batch.Queue(`
			INSERT INTO wallet_fills
			    (wallet_id, venue_id, market, exchange_fill_id, position_id,
			     filled_at, side, quantity, price, realized_pnl, fee, raw)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
			ON CONFLICT (venue_id, market, exchange_fill_id) DO NOTHING`,
			walletID, venueID, f.Market, f.ExchangeFillID, f.PositionID,
			f.FilledAt.UTC(), f.Side, f.Quantity, f.Price, f.RealizedPnl, f.Fee, f.Raw)
	}

	br := r.db.SendBatch(ctx, batch)
	defer br.Close()

	var created int64
	for range fills {
		tag, err := br.Exec()
		if err != nil {
			return created, err
		}
		created += tag.RowsAffected()
	}
	return created, nil
}

// LoadFills returns stored fills in [start, end), oldest-first.
func (r *FillRepository) LoadFills(ctx context.Context, walletID uuid.UUID, start, end time.Time) ([]PersistedFill, error) {
	rows, err := r.db.Query(ctx, `
		SELECT position_id, market, filled_at, side,
		       quantity::float8, price::float8, realized_pnl::float8
		FROM wallet_fills
		WHERE wallet_id = $1 AND filled_at >= $2 AND filled_at < $3
		ORDER BY filled_at ASC, id ASC`,
		walletID, start.UTC(), end.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []PersistedFill
	for rows.Next() {
		var p PersistedFill
		if err := rows.Scan(&p.PositionID, &p.Market, &p.Timestamp, &p.Side,
			&p.Quantity, &p.Price, &p.RealizedPnl); err != nil {
			return nil, err
		}
		p.Timestamp = p.Timestamp.UTC()
		out = append(out, p)
	}
	if out == nil {
		out = []PersistedFill{}
	}
	return out, rows.Err()
}

func floatOrNil(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}

func intOrNil(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

func timeOrNil(v *time.Time) any {
	if v == nil {
		return nil
	}
	return v.UTC()
}

// UpsertSnapshot writes the aggregate (market "") or per-market snapshot for
// a timeframe. NULL metrics stay NULL (BR-07); partial flags capped windows
// (ING-I-03).
func (r *FillRepository) UpsertSnapshot(ctx context.Context, walletID uuid.UUID, timeframe, market string, m *Metrics, partial bool) error {
	var marketArg any
	if market != "" {
		marketArg = market
	}

	tag, err := r.db.Exec(ctx, `
		UPDATE wallet_metric_snapshots
		SET realized_pnl = $3, roi = $4, win_rate = $5, volume = $6,
		    trade_count = $7, avg_position = $8, avg_leverage = $9,
		    long_count = $10, short_count = $11, last_active_at = $12,
		    is_partial = $13, computed_at = NOW()
		WHERE wallet_id = $1 AND timeframe = $2 AND venue_id IS NULL
		  AND ((market IS NULL AND $14::text IS NULL) OR market = $14::text)`,
		walletID, timeframe,
		floatOrNil(m.RealizedPnl), floatOrNil(m.Roi), floatOrNil(m.WinRate),
		floatOrNil(m.Volume), intOrNil(m.TradeCount), floatOrNil(m.AvgPosition),
		floatOrNil(m.AvgLeverage), intOrNil(m.LongCount), intOrNil(m.ShortCount),
		timeOrNil(m.LastActiveAt), partial, marketArg)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		return nil
	}

	_, err = r.db.Exec(ctx, `
		INSERT INTO wallet_metric_snapshots
		    (wallet_id, market, timeframe, realized_pnl, roi, win_rate, volume,
		     trade_count, avg_position, avg_leverage, long_count, short_count,
		     last_active_at, is_partial)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
		walletID, marketArg, timeframe,
		floatOrNil(m.RealizedPnl), floatOrNil(m.Roi), floatOrNil(m.WinRate),
		floatOrNil(m.Volume), intOrNil(m.TradeCount), floatOrNil(m.AvgPosition),
		floatOrNil(m.AvgLeverage), intOrNil(m.LongCount), intOrNil(m.ShortCount),
		timeOrNil(m.LastActiveAt), partial)
	return err
}

// RecomputeLegs re-derives logical-position legs over the wallet's full
// stored history (time order, per market: startPosition == 0 opens a new
// leg) and persists them. Leg indexes are fetch-local by nature — a wider
// window can reveal older fills that shift numbering — so legs are always
// recomputed globally before snapshots, keeping grouping stable for any
// window. Returns rows updated.
func (r *FillRepository) RecomputeLegs(ctx context.Context, walletID uuid.UUID) (int64, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, market, position_id, raw
		FROM wallet_fills
		WHERE wallet_id = $1
		ORDER BY filled_at ASC, id ASC`, walletID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	type row struct {
		id         uuid.UUID
		market     string
		positionID string
		raw        []byte
	}
	var all []row
	for rows.Next() {
		var rh row
		if err := rows.Scan(&rh.id, &rh.market, &rh.positionID, &rh.raw); err != nil {
			return 0, err
		}
		all = append(all, rh)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	legs := map[string]int{}
	batch := &pgx.Batch{}
	var queued int64
	for _, rh := range all {
		var payload struct {
			StartPosition string `json:"startPosition"`
		}
		if err := json.Unmarshal(rh.raw, &payload); err != nil {
			continue // keep existing position_id on corrupt payloads
		}
		startPos, err := strconv.ParseFloat(strings.TrimSpace(payload.StartPosition), 64)
		if err != nil {
			continue
		}
		if startPos == 0 {
			legs[rh.market]++
		}
		if legs[rh.market] == 0 {
			legs[rh.market] = 1
		}
		want := rh.market + "#" + itoa(legs[rh.market])
		if want == rh.positionID {
			continue
		}
		batch.Queue(`UPDATE wallet_fills SET position_id = $1 WHERE id = $2`, want, rh.id)
		queued++
	}
	if queued == 0 {
		return 0, nil
	}

	br := r.db.SendBatch(ctx, batch)
	defer br.Close()
	var updated int64
	for range make([]struct{}, queued) {
		tag, err := br.Exec()
		if err != nil {
			return updated, err
		}
		updated += tag.RowsAffected()
	}
	return updated, nil
}

func itoa(v int) string {
	return strconv.Itoa(v)
}
func (r *FillRepository) VenueIDByCode(ctx context.Context, code string) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.db.QueryRow(ctx, `SELECT id FROM venues WHERE code = $1`, code).Scan(&id)
	return id, err
}

// BackfillWallet lists EVM-style tracked wallets for venue backfill.
func (r *FillRepository) BackfillWallets(ctx context.Context) ([]Wallet, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, chain, address, COALESCE((SELECT code FROM venues v WHERE v.id = w.venue_id), ''),
		       first_seen_at, last_seen_at
		FROM tracked_wallets w
		WHERE chain = 'evm'
		ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Wallet
	for rows.Next() {
		var w Wallet
		if err := rows.Scan(&w.ID, &w.Chain, &w.Address, &w.Dex, &w.FirstSeenAt, &w.LastSeenAt); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	if out == nil {
		out = []Wallet{}
	}
	return out, rows.Err()
}
