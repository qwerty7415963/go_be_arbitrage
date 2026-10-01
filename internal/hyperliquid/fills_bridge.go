package hyperliquid

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/qwerty7415963/go_be_arbitrage/internal/trader"
)

// FetchTraderFills fetches normalized venue-agnostic fills for the sync engine.
// Rows with unparseable quantity/price/pnl, non-positive time or unknown side
// are skipped and counted; an invalid fee keeps the row with fee 0 (fee is
// informational). Order side comes from Side (B=buy, A=sell); position
// direction is derived downstream by trade reconstruction.
func (c *Client) FetchTraderFills(ctx context.Context, address string, startMs, endMs int64) ([]trader.Fill, bool, error) {
	raw, truncated, err := c.FetchAll(ctx, address, startMs, endMs)
	if err != nil {
		return nil, false, err
	}
	out := make([]trader.Fill, 0, len(raw))
	skipped := 0
	for _, f := range raw {
		qty, err := strconv.ParseFloat(strings.TrimSpace(f.Sz), 64)
		if err != nil || qty <= 0 {
			skipped++
			continue
		}
		price, err := strconv.ParseFloat(strings.TrimSpace(f.Px), 64)
		if err != nil || price < 0 {
			skipped++
			continue
		}
		closed, err := strconv.ParseFloat(strings.TrimSpace(f.ClosedPnl), 64)
		if err != nil {
			skipped++
			continue
		}
		fee, err := strconv.ParseFloat(strings.TrimSpace(f.Fee), 64)
		if err != nil {
			fee = 0
		}
		if f.Time <= 0 {
			skipped++
			continue
		}
		var buy bool
		switch f.Side {
		case "B":
			buy = true
		case "A":
			buy = false
		default:
			skipped++
			continue
		}
		coin := strings.ToUpper(strings.TrimSpace(f.Coin))
		if coin == "" {
			skipped++
			continue
		}
		out = append(out, trader.Fill{
			Market: coin, Tid: f.Tid,
			FilledAt:  time.UnixMilli(f.Time).UTC(),
			Buy:       buy,
			Quantity:  qty,
			Price:     price,
			ClosedPnL: closed,
			Fee:       fee,
		})
	}
	return out, truncated, nil
}
