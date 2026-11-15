package assetlogo

import (
	"strings"

	"github.com/google/uuid"
)

func NormalizeSymbol(symbol string) string {
	return strings.ToUpper(strings.TrimSpace(symbol))
}

func CanonicalKeyCryptoNative(symbol string) string {
	return "crypto:native:" + NormalizeSymbol(symbol)
}

func CanonicalKeyCryptoToken(chain, contract string) string {
	return "crypto:token:" + strings.ToLower(strings.TrimSpace(chain)) + ":" + strings.ToLower(strings.TrimSpace(contract))
}

func CanonicalKeyIndex(symbol string) string {
	return "index:" + NormalizeSymbol(symbol)
}

func CanonicalKeyCommodity(symbol string) string {
	return "commodity:" + NormalizeSymbol(symbol)
}

func CanonicalKeyEquity(exchange, symbol string) string {
	return "equity:" + strings.ToUpper(strings.TrimSpace(exchange)) + ":" + NormalizeSymbol(symbol)
}

func CanonicalKeyUnknown(symbol string) string {
	return "unknown:" + NormalizeSymbol(symbol)
}

func IsValidAssetClass(v string) bool {
	return AssetClass(v).Valid()
}

func StripDexPrefix(symbol string) string {
	// e.g. "xyz:NVDA" -> "NVDA"? keep simple: if contains ':' return part after last ':'
	if i := strings.LastIndex(symbol, ":"); i >= 0 {
		return symbol[i+1:]
	}
	return symbol
}

func ResolveAliasAssetID(alias *AssetAlias) uuid.UUID {
	if alias == nil {
		return uuid.Nil
	}
	return alias.AssetID
}
