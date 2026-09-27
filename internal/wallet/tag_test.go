package wallet

import (
	"strings"
	"testing"
)

// TAG-U-01: tag normalization — trim, 100-rune cap, blank means clear.
func TestNormalizeTag(t *testing.T) {
	tag, clear, ferr := NormalizeTag("  Binance hot  ")
	if ferr != nil || clear || tag != "Binance hot" {
		t.Errorf("trim: got %q clear=%v err=%v", tag, clear, ferr)
	}

	if _, clear, ferr := NormalizeTag("   "); ferr != nil || !clear {
		t.Errorf("blank must signal clear: clear=%v err=%v", clear, ferr)
	}

	long := strings.Repeat("a", MaxTagRunes+1)
	if _, _, ferr := NormalizeTag(long); ferr == nil {
		t.Error("expected COMMON-902 for over-long tag")
	} else if ferr.Field != "tag" || ferr.Code != "COMMON-902" {
		t.Errorf("got %+v", ferr)
	}

	ok := strings.Repeat("é", MaxTagRunes) // rune (not byte) counting
	if tag, clear, ferr := NormalizeTag(ok); ferr != nil || clear || tag != ok {
		t.Errorf("boundary: got clear=%v err=%v", clear, ferr)
	}
}
