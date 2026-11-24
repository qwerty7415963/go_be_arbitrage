package trader

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
)

// WalletEventInput is one row to be upserted into trader_wallet_event.
type WalletEventInput struct {
	VenueID     uuid.UUID
	Addr        string
	Market      string
	EventType   string
	SizeDelta   float64
	Price       float64
	RealizedPnl float64
	OccurredAt  time.Time
	DedupKey    string
}

// ClassifyWalletEvents reads a sequence of fills (ideally ordered by
// (market, filled_at, exchange_tid)) and reconstructs the signed net position
// per market, classifying each fill's effect on that position.
func ClassifyWalletEvents(fills []Fill, venueID uuid.UUID, addr string) []WalletEventInput {
	ordered := append([]Fill(nil), fills...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Market != ordered[j].Market {
			return ordered[i].Market < ordered[j].Market
		}
		if !ordered[i].FilledAt.Equal(ordered[j].FilledAt) {
			return ordered[i].FilledAt.Before(ordered[j].FilledAt)
		}
		return ordered[i].Tid < ordered[j].Tid
	})

	pos := map[string]float64{}
	var out []WalletEventInput
	for _, f := range ordered {
		if f.Quantity == 0 {
			continue
		}
		q := f.Quantity
		if !f.Buy {
			q = -q
		}
		before := pos[f.Market]
		after := before + q

		var eventType string
		switch {
		case before == 0 && after != 0:
			eventType = "position_opened"
		case before != 0 && after == 0:
			eventType = "position_closed"
		case before != 0 && after != 0 && sgn(before) == sgn(after):
			if abs(after) > abs(before) {
				eventType = "position_increased"
			} else if abs(after) < abs(before) {
				eventType = "position_reduced"
			}
		}

		if eventType != "" {
			out = append(out, WalletEventInput{
				VenueID:     venueID,
				Addr:        addr,
				Market:      f.Market,
				EventType:   eventType,
				SizeDelta:   q,
				Price:       f.Price,
				RealizedPnl: f.ClosedPnL,
				OccurredAt:  f.FilledAt.UTC(),
				DedupKey:    fmt.Sprintf("%s|%s|%s|%d|%s", venueID, addr, f.Market, f.Tid, eventType),
			})
		}
		pos[f.Market] = after
	}
	return out
}

// Aliases for callers/tests that use a different name.
var (
	ClassifyFills         = ClassifyWalletEvents
	WalletEventsFromFills = ClassifyWalletEvents
)

func loadWalletBufferFills(ctx context.Context, repo *Repository, venueID uuid.UUID, addr string) ([]Fill, error) {
	rows, err := repo.pool.Query(ctx, `
		SELECT market, exchange_tid, filled_at, side, quantity, price, closed_pnl, fee
		FROM trader_fill_buffer
		WHERE venue_id = $1 AND wallet_address = $2
		ORDER BY market ASC, filled_at ASC, exchange_tid ASC`,
		venueID, addr)
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

// ProjectWalletEvents rebuilds wallet events for a wallet from its buffered fills.
func ProjectWalletEvents(ctx context.Context, repo *Repository, venueID uuid.UUID, addr string) error {
	fills, err := loadWalletBufferFills(ctx, repo, venueID, addr)
	if err != nil {
		return err
	}
	for _, ev := range ClassifyWalletEvents(fills, venueID, addr) {
		if err := repo.UpsertWalletEvent(ctx, venueID, addr, ev.Market, ev.EventType,
			ev.SizeDelta, ev.Price, ev.RealizedPnl, ev.OccurredAt, ev.DedupKey); err != nil {
			return err
		}
	}
	return nil
}

// BackfillWalletEvents is an alias for ProjectWalletEvents (full rebuild from buffer).
func BackfillWalletEvents(ctx context.Context, repo *Repository, venueID uuid.UUID, addr string) error {
	return ProjectWalletEvents(ctx, repo, venueID, addr)
}
