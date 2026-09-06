package orderbook

import (
	"math/big"
	"strings"
)

func calculateSpread(bestBid, bestAsk string) string {
	bid, _ := new(big.Float).SetString(bestBid)
	ask, _ := new(big.Float).SetString(bestAsk)

	spread := new(big.Float).Sub(ask, bid)
	return trimTrailingZeros(spread.Text('f', 8))
}

func calculateNotional(price, quantity string) string {
	p, _ := new(big.Float).SetString(price)
	q, _ := new(big.Float).SetString(quantity)

	notional := new(big.Float).Mul(p, q)
	return trimTrailingZeros(notional.Text('f', 8))
}

func trimTrailingZeros(s string) string {
	if strings.Contains(s, ".") {
		s = strings.TrimRight(s, "0")
		s = strings.TrimRight(s, ".")
	}
	if s == "" {
		return "0"
	}
	return s
}

func PriceLevelEqual(a, b PriceLevel) bool {
	return a.Price == b.Price && a.Quantity == b.Quantity
}

func PriceLevelsEqual(a, b []PriceLevel) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !PriceLevelEqual(a[i], b[i]) {
			return false
		}
	}
	return true
}

func ValidatePriceLevel(level PriceLevel) bool {
	if level.Price == "" || level.Quantity == "" {
		return false
	}
	if _, ok := new(big.Float).SetString(level.Price); !ok {
		return false
	}
	if _, ok := new(big.Float).SetString(level.Quantity); !ok {
		return false
	}
	return true
}

func ValidateSnapshot(snapshot *OrderBookSnapshot) bool {
	if snapshot == nil {
		return false
	}
	for _, bid := range snapshot.Bids {
		if !ValidatePriceLevel(bid) {
			return false
		}
	}
	for _, ask := range snapshot.Asks {
		if !ValidatePriceLevel(ask) {
			return false
		}
	}
	return true
}

func ValidateDelta(delta *OrderBookDelta) bool {
	if delta == nil {
		return false
	}
	if delta.ToSequence < delta.FromSequence {
		return false
	}
	return true
}
