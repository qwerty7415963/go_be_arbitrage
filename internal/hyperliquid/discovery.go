package hyperliquid

import (
	"context"

	"github.com/qwerty7415963/go_be_arbitrage/internal/trader"
)

// Compile-time guarantees: *Client satisfies the venue-agnostic seams.
var (
	_ trader.DiscoveryFetcher = (*Client)(nil)
	_ trader.FillFetcher      = (*Client)(nil)
)

// FetchTop maps leaderboard rows onto the venue-agnostic discovery
// shape (limit<=0 returns the whole board; top-N selection is the caller's).
func (c *Client) FetchTop(ctx context.Context, limit int) ([]trader.DiscoveredWallet, error) {
	rows, err := c.FetchLeaderboard(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]trader.DiscoveredWallet, 0, len(rows))
	for _, r := range rows {
		w := trader.DiscoveredWallet{
			Address:      r.Address,
			DisplayName:  r.DisplayName,
			AccountValue: r.AccountValue,
			Windows:      map[string]trader.WindowStats{},
		}
		for name, win := range r.Windows {
			w.Windows[name] = trader.WindowStats{PnL: win.PnL, ROI: win.ROI, Volume: win.Volume}
		}
		out = append(out, w)
	}
	return out, nil
}
