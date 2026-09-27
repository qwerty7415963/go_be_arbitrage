package hyperliquid

import (
	"encoding/json"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/qwerty7415963/go_be_arbitrage/internal/wallet"
)

func msToTime(ms int64) time.Time {
	return time.UnixMilli(ms).UTC()
}

// NormalizeFills converts raw venue fills (any order) into engine-ready
// wallet.FillInput rows oldest-first, deriving logical-position legs per
// coin: a fill whose startPosition parses to zero opens a new leg
// (PositionID "COIN#n"); subsequent fills share the leg, so partial closes
// count as 1 trade (locked decision). Net PnL = closedPnl − fee (a negative
// fee is a rebate and increases net). Rows with unparseable numerics or an
// unknown side are skipped and counted (ING-U-02); the caller logs the count.
func NormalizeFills(fills []Fill) ([]wallet.FillInput, int) {
	ordered := make([]Fill, len(fills))
	copy(ordered, fills)
	sortFillsAsc(ordered)

	legs := map[string]int{} // coin -> current leg index
	out := make([]wallet.FillInput, 0, len(ordered))
	skipped := 0

	for _, f := range ordered {
		qty, err := strconv.ParseFloat(strings.TrimSpace(f.Sz), 64)
		if err != nil || qty < 0 {
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
			fee = 0 // fee is informational; missing/invalid fee keeps the row
		}
		if f.Time <= 0 {
			skipped++
			continue
		}
		side, ok := mapSide(f)
		if !ok {
			skipped++
			continue
		}
		startPos, err := strconv.ParseFloat(strings.TrimSpace(f.StartPosition), 64)
		if err != nil {
			skipped++
			continue
		}

		coin := strings.ToUpper(strings.TrimSpace(f.Coin))
		if startPos == 0 {
			legs[coin]++ // flat before this fill → new logical leg
		}
		if legs[coin] == 0 {
			legs[coin] = 1 // first-seen leg (history starts mid-position)
		}

		raw, merr := json.Marshal(f)
		if merr != nil {
			log.Printf("hyperliquid normalize: marshal fill: %v", merr)
			skipped++
			continue
		}

		out = append(out, wallet.FillInput{
			PositionID:     coin + "#" + itoa(int64(legs[coin])),
			Market:         coin,
			ExchangeFillID: itoa(f.Tid),
			FilledAt:       msToTime(f.Time),
			Side:           side,
			Quantity:       qty,
			Price:          price,
			RealizedPnl:    closed - fee,
			Fee:            fee,
			Raw:            raw,
		})
	}
	return out, skipped
}

// mapSide resolves LONG/SHORT from the venue direction label, falling back
// to the order side (B=buy→LONG, A=ask/sell→SHORT).
func mapSide(f Fill) (string, bool) {
	if strings.Contains(f.Dir, "Long") {
		return "LONG", true
	}
	if strings.Contains(f.Dir, "Short") {
		return "SHORT", true
	}
	switch f.Side {
	case "B":
		return "LONG", true
	case "A":
		return "SHORT", true
	}
	return "", false
}
