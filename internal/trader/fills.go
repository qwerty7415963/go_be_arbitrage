package trader

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Fill is one normalized venue fill (venue-agnostic). Buy adds to the signed
// position, sell subtracts; closedPnl is the venue-reported realized PnL of
// this fill (funding excluded upstream — tracked separately, never decides
// win/loss per spec §10). Raw payloads are never stored.
type Fill struct {
	Market    string
	Tid       int64
	FilledAt  time.Time
	Buy       bool
	Quantity  float64
	Price     float64
	ClosedPnL float64
	Fee       float64
}

// FillFetcher is the venue fill seam: normalized fills for an address in
// [startMs, endMs]; truncated means the venue capped the window (older data
// hidden) and downstream snapshots must be flagged partial.
type FillFetcher interface {
	FetchFills(ctx context.Context, address string, startMs, endMs int64) ([]Fill, bool, error)
}

// UpsertBufferFills stages fills idempotently; returns the new-row count.
func (r *Repository) UpsertBufferFills(ctx context.Context, venueID uuid.UUID, addr string, fills []Fill) (int64, error) {
	if len(fills) == 0 {
		return 0, nil
	}
	var n int64
	for _, f := range fills {
		tag, err := r.pool.Exec(ctx, `
			INSERT INTO trader_fill_buffer
				(venue_id, wallet_address, market, exchange_tid, filled_at, side,
				 quantity, price, closed_pnl, fee)
			VALUES ($1,$2,$3,$4,$5, CASE WHEN $6 THEN 'BUY' ELSE 'SELL' END, $7,$8,$9,$10)
			ON CONFLICT (venue_id, wallet_address, market, exchange_tid) DO NOTHING`,
			venueID, addr, f.Market, f.Tid, f.FilledAt.UTC(), f.Buy,
			f.Quantity, f.Price, f.ClosedPnL, f.Fee)
		if err != nil {
			return n, err
		}
		n += tag.RowsAffected()
	}
	return n, nil
}

// LoadBufferFills returns staged fills in [start, end] oldest-first.
func (r *Repository) LoadBufferFills(ctx context.Context, venueID uuid.UUID, addr string, start, end time.Time) ([]Fill, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT market, exchange_tid, filled_at, side, quantity, price, closed_pnl, fee
		FROM trader_fill_buffer
		WHERE venue_id = $1 AND wallet_address = $2
		  AND filled_at >= $3 AND filled_at < $4
		ORDER BY filled_at ASC, exchange_tid ASC`,
		venueID, addr, start.UTC(), end.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Fill
	for rows.Next() {
		var f Fill
		var side string
		if err := rows.Scan(&f.Market, &f.Tid, &f.FilledAt, &side,
			&f.Quantity, &f.Price, &f.ClosedPnL, &f.Fee); err != nil {
			return nil, err
		}
		f.Buy = side == "BUY"
		out = append(out, f)
	}
	return out, rows.Err()
}

// PurgeBuffer deletes staged fills older than cutoff (bounded retention:
// raw fills never persist permanently, spec non-goal). Returns deleted rows.
func (r *Repository) PurgeBuffer(ctx context.Context, venueID uuid.UUID, addr string, cutoff time.Time) (int64, error) {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM trader_fill_buffer
		WHERE venue_id = $1 AND wallet_address = $2 AND filled_at < $3`,
		venueID, addr, cutoff.UTC())
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// backoffDelay returns the wait before the next attempt after retry failures:
// 5min doubling per retry capped at 6h (spec BE-010: retries respect backoff).
func backoffDelay(retry int) time.Duration {
	d := 5 * time.Minute
	for i := 0; i < retry; i++ {
		d *= 2
		if d >= 6*time.Hour {
			return 6 * time.Hour
		}
	}
	return d
}
