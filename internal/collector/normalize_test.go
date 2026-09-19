package collector

import "testing"

func TestNormalizeBaseAsset(t *testing.T) {
	tests := []struct {
		name      string
		symbol    string
		venueCode string
		want      string
	}{
		{"binance BTCUSDT", "BTCUSDT", "binance", "BTC"},
		{"binance ETHUSDT", "ETHUSDT", "binance", "ETH"},
		{"binance 1000PEPEUSDT", "1000PEPEUSDT", "binance", "1000PEPE"},
		{"binance SOLUSDT", "SOLUSDT", "binance", "SOL"},
		{"binance BTCBUSD matches USD first", "BTCBUSD", "binance", "BTCB"},
		{"binance ETHUSDC", "ETHUSDC", "binance", "ETH"},
		{"binance ETHBTC", "ETHBTC", "binance", "ETH"},
		{"binance AVAXETH", "AVAXETH", "binance", "AVAX"},
		{"binance BNBBTC", "BNBBTC", "binance", "BNB"},
		{"binance ADAUSD", "ADAUSD", "binance", "ADA"},
		{"extended BTC-USD", "BTC-USD", "extended", "BTC"},
		{"extended ETH-USD", "ETH-USD", "extended", "ETH"},
		{"extended SOL-USDC", "SOL-USDC", "extended", "SOL"},
		{"extended no hyphen", "BTCUSD", "extended", "BTCUSD"},
		{"variational BTCUSDT", "BTCUSDT", "variational", "BTC"},
		{"variational DOGEUSDT", "DOGEUSDT", "variational", "DOGE"},
		{"empty symbol", "", "binance", ""},
		{"empty symbol extended", "", "extended", ""},
		{"single char no suffix", "A", "binance", "A"},
		{"only suffix USDT", "USDT", "binance", "USDT"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeBaseAsset(tt.symbol, tt.venueCode)
			if got != tt.want {
				t.Errorf("NormalizeBaseAsset(%q, %q) = %q, want %q", tt.symbol, tt.venueCode, got, tt.want)
			}
		})
	}
}

func TestNormalizeQuoteAsset(t *testing.T) {
	tests := []struct {
		name      string
		symbol    string
		venueCode string
		want      string
	}{
		{"binance BTCUSDT", "BTCUSDT", "binance", "USDT"},
		{"binance ETHUSDT", "ETHUSDT", "binance", "USDT"},
		{"binance BTCBUSD", "BTCBUSD", "binance", "BUSD"},
		{"binance ETHUSDC", "ETHUSDC", "binance", "USDC"},
		{"binance ETHBTC", "ETHBTC", "binance", "BTC"},
		{"binance AVAXETH", "AVAXETH", "binance", "ETH"},
		{"binance BNBBTC", "BNBBTC", "binance", "BTC"},
		{"binance ADAUSD", "ADAUSD", "binance", "USD"},
		{"extended BTC-USD", "BTC-USD", "extended", "USD"},
		{"extended ETH-USD", "ETH-USD", "extended", "USD"},
		{"extended SOL-USDC", "SOL-USDC", "extended", "USDC"},
		{"extended no hyphen defaults", "BTCUSD", "extended", "USD"},
		{"variational BTCUSDT", "BTCUSDT", "variational", "USDT"},
		{"variational DOGEUSDT", "DOGEUSDT", "variational", "USDT"},
		{"empty symbol binance", "", "binance", "USD"},
		{"empty symbol extended", "", "extended", "USD"},
		{"single char no suffix", "A", "binance", "USD"},
		{"only suffix USDT", "USDT", "binance", "USD"},
		{"hyphen at start extended", "-USD", "extended", "USD"},
		{"hyphen at end extended", "BTC-", "extended", "USD"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeQuoteAsset(tt.symbol, tt.venueCode)
			if got != tt.want {
				t.Errorf("NormalizeQuoteAsset(%q, %q) = %q, want %q", tt.symbol, tt.venueCode, got, tt.want)
			}
		})
	}
}

func TestNormalizeBaseAssetEdgeCases(t *testing.T) {
	got := NormalizeBaseAsset("1000PEPEUSDT", "binance")
	if got != "1000PEPE" {
		t.Errorf("prefix-digit asset: got %q, want %q", got, "1000PEPE")
	}

	got = NormalizeBaseAsset("1INCHUSDT", "binance")
	if got != "1INCH" {
		t.Errorf("1INCH asset: got %q, want %q", got, "1INCH")
	}
}

func TestNormalizeQuoteAssetEdgeCases(t *testing.T) {
	got := NormalizeQuoteAsset("BTCUSDT", "unknown_venue")
	if got != "USDT" {
		t.Errorf("unknown venue default path: got %q, want %q", got, "USDT")
	}

	got = NormalizeQuoteAsset("BTCUSD", "unknown_venue")
	if got != "USD" {
		t.Errorf("unknown venue with USD: got %q, want %q", got, "USD")
	}
}
