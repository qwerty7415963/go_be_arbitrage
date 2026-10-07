package trader

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// Activity cursor roundtrip + tamper + foreign-query rejection.
func TestActivityCursor_Roundtrip(t *testing.T) {
	secret := []byte("test-secret")
	fp := activityFingerprint(uuid.New(), "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	ct := time.Now().UTC().Truncate(time.Microsecond)
	ot := ct.Add(-time.Hour)
	cur, err := EncodeActivityCursor(secret, fp, ct, "BTC", ot)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	gct, gm, got, err := DecodeActivityCursor(secret, fp, cur)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !gct.Equal(ct) || gm != "BTC" || !got.Equal(ot) {
		t.Errorf("roundtrip: %v %s %v", gct, gm, got)
	}
	if _, _, _, err := DecodeActivityCursor(secret, fp, "forged.cursor"); err == nil {
		t.Error("forged cursor must fail")
	}
	tampered := cur[:len(cur)-2] + "xx"
	if _, _, _, err := DecodeActivityCursor(secret, fp, tampered); err == nil {
		t.Error("tampered signature must fail")
	}
	otherFP := activityFingerprint(uuid.New(), "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	if _, _, _, err := DecodeActivityCursor(secret, otherFP, cur); err == nil {
		t.Error("foreign-query cursor must fail")
	}
}
