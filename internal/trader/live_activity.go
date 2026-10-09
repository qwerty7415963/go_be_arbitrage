package trader

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/qwerty7415963/go_be_arbitrage/internal/hyperliquid"
)

// LiveActivityWindow is the trailing window for §1.2 (30d).
const LiveActivityWindow = 30 * 24 * time.Hour

// LiveActivitySnapshot is the cached 30d universe for one wallet:
// reconstructed trades with funding attribution + fetch metadata.
// Cached 12s per (wallet,venue) + single-flight; per-request sort/filter/
// pagination runs in-memory over Rows (reuse helpers).
type LiveActivitySnapshot struct {
	Rows    []CompletedTradeRow
	AsOf    time.Time
	Partial bool
}

// mapHLFillsToTrader maps raw userFillsByTime rows onto venue-agnostic fills.
// Same skip rules as HLFillAdapter (unparseable qty/price/pnl, non-positive
// time, unknown side skipped; invalid fee keeps the row with fee 0).
func mapHLFillsToTrader(raw []hyperliquid.Fill) []Fill {
	out := make([]Fill, 0, len(raw))
	for _, f := range raw {
		qty, err := strconv.ParseFloat(strings.TrimSpace(f.Sz), 64)
		if err != nil || qty <= 0 {
			continue
		}
		price, err := strconv.ParseFloat(strings.TrimSpace(f.Px), 64)
		if err != nil || price < 0 {
			continue
		}
		closed, err := strconv.ParseFloat(strings.TrimSpace(f.ClosedPnl), 64)
		if err != nil {
			continue
		}
		fee, err := strconv.ParseFloat(strings.TrimSpace(f.Fee), 64)
		if err != nil {
			fee = 0
		}
		if f.Time <= 0 {
			continue
		}
		var buy bool
		switch f.Side {
		case "B":
			buy = true
		case "A":
			buy = false
		default:
			continue
		}
		coin := strings.ToUpper(strings.TrimSpace(f.Coin))
		if coin == "" {
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
	return out
}

// parseFundingPayments normalizes userFunding updates into payments:
// coin upper-cased, usdc parsed (unparseable rows skipped), time > 0 required.
// Signed usdc (negative = paid) is kept as-is; attribution sums per coin.
func parseFundingPayments(updates []hyperliquid.FundingUpdate) []hyperliquid.FundingPayment {
	out := make([]hyperliquid.FundingPayment, 0, len(updates))
	for _, u := range updates {
		if u.Time <= 0 {
			continue
		}
		coin := strings.ToUpper(strings.TrimSpace(u.Delta.Coin))
		if coin == "" {
			continue
		}
		usdc, err := strconv.ParseFloat(strings.TrimSpace(u.Delta.Usdc), 64)
		if err != nil {
			continue
		}
		out = append(out, hyperliquid.FundingPayment{
			Time: time.UnixMilli(u.Time).UTC(),
			Coin: coin,
			Usdc: usdc,
		})
	}
	return out
}

// attributeFunding sums funding payments with openTime ≤ time ≤ closeTime per
// coin (LIVE-CONTRACT v1.2 §1.2). Informational only: callers never use it for
// win/loss/counts or net. Returns parallel sums aligned with trades.
func attributeFunding(trades []CompletedTrade, payments []hyperliquid.FundingPayment) []float64 {
	sums := make([]float64, len(trades))
	if len(trades) == 0 || len(payments) == 0 {
		return sums
	}
	// Group payments by coin for linear scans (windows are small: hundreds).
	byCoin := map[string][]hyperliquid.FundingPayment{}
	for _, p := range payments {
		byCoin[p.Coin] = append(byCoin[p.Coin], p)
	}
	for i, t := range trades {
		var sum float64
		for _, p := range byCoin[t.Market] {
			if !p.Time.Before(t.OpenTime) && !p.Time.After(t.CloseTime) {
				sum += p.Usdc
			}
		}
		sums[i] = sum
	}
	return sums
}

// completedTradesToRows converts reconstructed cycles to live rows with
// funding attached. Side LONG when cycle long else SHORT.
func completedTradesToRows(trades []CompletedTrade, funding []float64) []CompletedTradeRow {
	rows := make([]CompletedTradeRow, 0, len(trades))
	for i, t := range trades {
		side := "SHORT"
		if t.Long {
			side = "LONG"
		}
		var f float64
		if i < len(funding) {
			f = funding[i]
		}
		rows = append(rows, CompletedTradeRow{
			Market: t.Market, Side: side,
			OpenedAt: t.OpenTime.UTC(), ClosedAt: t.CloseTime.UTC(),
			Volume: t.Volume, PnL: t.PnL, Fees: t.Fees, Funding: f,
			Fills: t.Fills, EntryPrice: t.EntryPrice, ExitPrice: t.ExitPrice,
			Size: t.Size,
		})
	}
	return rows
}

// fetchLiveActivity fetches the 30d fills window + funding, reconstructs via
// ReconstructTrades (reuse) and attributes funding. truncated (venue 10k cap)
// flows to partial. Funding failure degrades to 0 (informational): the fills
// universe still returns ready.
func fetchLiveActivity(ctx context.Context, client OnDemandClient, addr string, now time.Time) (*LiveActivitySnapshot, error) {
	if client == nil {
		return nil, errUpstreamUnavailable
	}
	startMs := now.Add(-LiveActivityWindow).UnixMilli()
	endMs := now.UnixMilli()
	raw, truncated, err := client.FetchFillsWindow(ctx, addr, startMs, endMs)
	if err != nil {
		return nil, err
	}
	fills := mapHLFillsToTrader(raw)
	trades := ReconstructTrades(fills)
	var payments []hyperliquid.FundingPayment
	if fraw, ferr := client.FetchUserFunding(ctx, addr, startMs, endMs); ferr == nil {
		payments = parseFundingPayments(fraw)
	}
	// Funding error ⇒ 0 attribution (informational), still ready.
	funding := attributeFunding(trades, payments)
	return &LiveActivitySnapshot{
		Rows:    completedTradesToRows(trades, funding),
		AsOf:    now.UTC(),
		Partial: truncated,
	}, nil
}
