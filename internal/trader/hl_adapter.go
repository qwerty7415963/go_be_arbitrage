package trader

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/qwerty7415963/go_be_arbitrage/internal/hyperliquid"
)

// HLDiscoveryAdapter maps Hyperliquid leaderboard rows onto the
// venue-agnostic discovery shape (kept here so the hyperliquid package stays
// raw-API-only with no dependency on trader).
type HLDiscoveryAdapter struct {
	C *hyperliquid.Client
}

var _ DiscoveryFetcher = HLDiscoveryAdapter{}

func (a HLDiscoveryAdapter) FetchTop(ctx context.Context, limit int) ([]DiscoveredWallet, error) {
	rows, err := a.C.FetchLeaderboard(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]DiscoveredWallet, 0, len(rows))
	for _, r := range rows {
		w := DiscoveredWallet{
			Address:      r.Address,
			DisplayName:  r.DisplayName,
			AccountValue: r.AccountValue,
			Windows:      map[string]WindowStats{},
		}
		for name, win := range r.Windows {
			w.Windows[name] = WindowStats{PnL: win.PnL, ROI: win.ROI, Volume: win.Volume}
		}
		out = append(out, w)
	}
	return out, nil
}

// HLFillAdapter maps raw Hyperliquid fills onto venue-agnostic fills.
// Rows with unparseable quantity/price/pnl, non-positive time or unknown side
// are skipped and counted; an invalid fee keeps the row with fee 0 (fee is
// informational). Order side comes from Side (B=buy, A=sell); position
// direction is derived downstream by trade reconstruction.
type HLFillAdapter struct {
	C *hyperliquid.Client
}

var _ FillFetcher = HLFillAdapter{}

func (a HLFillAdapter) FetchTraderFills(ctx context.Context, address string, startMs, endMs int64) ([]Fill, bool, error) {
	raw, truncated, err := a.C.FetchAll(ctx, address, startMs, endMs)
	if err != nil {
		return nil, false, err
	}
	out := make([]Fill, 0, len(raw))
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
		out = append(out, Fill{
			Market: coin, Tid: f.Tid,
			FilledAt:  time.UnixMilli(f.Time).UTC(),
			Buy:       buy,
			Quantity:  qty,
			Price:     price,
			ClosedPnL: closed,
			Fee:       fee,
		})
	}
	_ = skipped
	return out, truncated, nil
}
