package collector

import (
	"strings"
)

// NormalizeBaseAsset normalizes a venue symbol to a canonical base asset.
// Examples:
//
//	Binance:   "BTCUSDT" → "BTC", "1000PEPEUSDT" → "1000PEPE"
//	Extended:  "BTC-USD" → "BTC" (already clean via AssetName)
//	Variational: "BTCUSDT" → "BTC" (already extracted in adapter)
func NormalizeBaseAsset(symbol, venueCode string) string {
	switch venueCode {
	case "extended":
		// Extended symbols are like "BTC-USD", extract before hyphen
		if idx := strings.Index(symbol, "-"); idx > 0 {
			return symbol[:idx]
		}
		return symbol
	default:
		// Binance-style: BTCUSDT, 1000PEPEUSDT, etc.
		// Strip common quote asset suffixes
		for _, suffix := range []string{"USDT", "USD", "BUSD", "USDC", "BTC", "ETH", "BNB"} {
			if strings.HasSuffix(symbol, suffix) && len(symbol) > len(suffix) {
				return strings.TrimSuffix(symbol, suffix)
			}
		}
		return symbol
	}
}

// NormalizeQuoteAsset extracts the quote asset from a venue symbol.
func NormalizeQuoteAsset(symbol, venueCode string) string {
	switch venueCode {
	case "extended":
		// Extended symbols are like "BTC-USD", extract after hyphen
		if idx := strings.Index(symbol, "-"); idx > 0 && idx < len(symbol)-1 {
			return symbol[idx+1:]
		}
		return "USD"
	default:
		// Binance-style: detect quote asset
		for _, suffix := range []string{"USDT", "BUSD", "USDC", "BTC", "ETH", "BNB"} {
			if strings.HasSuffix(symbol, suffix) {
				return suffix
			}
		}
		return "USD"
	}
}
