package assetlogo

import (
	"strings"
	"testing"
)

func TestCanonicalKey_Format(t *testing.T) {
	if CanonicalKeyCryptoNative("btc") != "crypto:native:BTC" {
		t.Fatal("native")
	}
	if CanonicalKeyCryptoToken("ethereum", "0xabc") != "crypto:token:ethereum:0xabc" {
		t.Fatal("token")
	}
	if CanonicalKeyIndex("SP500") != "index:SP500" {
		t.Fatal("index")
	}
	if CanonicalKeyEquity("NASDAQ", "nvda") != "equity:NASDAQ:NVDA" {
		t.Fatal("equity")
	}
	if CanonicalKeyUnknown("foo") != "unknown:FOO" {
		t.Fatal("unknown")
	}
}

func TestIsValidAssetClass(t *testing.T) {
	if !IsValidAssetClass("crypto_native") {
		t.Fatal("valid")
	}
	if IsValidAssetClass("nope") {
		t.Fatal("invalid")
	}
}

func TestStripDexPrefix(t *testing.T) {
	if StripDexPrefix("xyz:NVDA") != "NVDA" {
		t.Fatal("strip")
	}
	if StripDexPrefix("BTC") != "BTC" {
		t.Fatal("no strip")
	}
	_ = strings.ToUpper
}
