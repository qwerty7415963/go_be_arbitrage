package trader

import (
	"errors"
	"testing"

	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

func mustCode(t *testing.T, err error) domain.ErrorCode {
	t.Helper()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var appErr *domain.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected AppError, got %T", err)
	}
	return appErr.Code
}

// VAL-U-01: defaults + every validation rule.
func TestSearchRequest_NormalizeValidate(t *testing.T) {
	r := &SearchRequest{}
	r.Normalize()
	if r.Period != Period30D || r.Venue != DefaultVenue || r.SortBy != "pnl" ||
		r.SortDirection != "desc" || r.Limit != 50 {
		t.Errorf("bad defaults: %+v", r)
	}
	if err := r.Validate(); err != nil {
		t.Errorf("default request must validate: %v", err)
	}

	f := func() *SearchRequest { r := &SearchRequest{}; r.Normalize(); return r }

	cases := []struct {
		name string
		mut  func(*SearchRequest)
	}{
		{"bad period", func(r *SearchRequest) { r.Period = "90D" }},
		{"roi range", func(r *SearchRequest) { r.ROIMin = f64(30); r.ROIMax = f64(10) }},
		{"win_rate range", func(r *SearchRequest) { r.WinRateMin = f64(60); r.WinRateMax = f64(10) }},
		{"pnl range", func(r *SearchRequest) { r.PnLMin = f64(5); r.PnLMax = f64(-5) }},
		{"volume range", func(r *SearchRequest) { r.VolumeMin = f64(9); r.VolumeMax = f64(1) }},
		{"trades range", func(r *SearchRequest) { r.TradeCountMin = i(9); r.TradeCountMax = i(1) }},
		{"pf range", func(r *SearchRequest) { r.ProfitFactorMin = f64(2); r.ProfitFactorMax = f64(1) }},
		{"long wr range", func(r *SearchRequest) { r.LongWinRateMin = f64(2); r.LongWinRateMax = f64(1) }},
		{"short wr range", func(r *SearchRequest) { r.ShortWinRateMin = f64(2); r.ShortWinRateMax = f64(1) }},
		{"win_rate > 100", func(r *SearchRequest) { r.WinRateMax = f64(101) }},
		{"win_rate < 0", func(r *SearchRequest) { r.WinRateMin = f64(-1) }},
		{"volume negative", func(r *SearchRequest) { r.VolumeMin = f64(-1) }},
		{"trades negative", func(r *SearchRequest) { r.TradeCountMin = i(-1) }},
		{"pf negative", func(r *SearchRequest) { r.ProfitFactorMin = f64(-0.5) }},
		{"bad timestamp", func(r *SearchRequest) { r.LastTradeAfter = "yesterday" }},
		{"bad group", func(r *SearchRequest) { r.GroupID = "nope" }},
		{"bad sort", func(r *SearchRequest) { r.SortBy = "avg_leverage" }},
		{"bad dir", func(r *SearchRequest) { r.SortDirection = "sideways" }},
		{"limit 0", func(r *SearchRequest) { r.Limit = 0; r.Normalize(); r.Limit = 0 }},
		{"limit 101", func(r *SearchRequest) { r.Limit = 101 }},
	}
	for _, tc := range cases {
		req := f()
		tc.mut(req)
		if code := mustCode(t, req.Validate()); code != domain.ErrCodeInvalidFilter {
			t.Errorf("%s: want INVALID_FILTER, got %s", tc.name, code)
		}
	}

	// Zero is valid where allowed (BE-039): pnl=0, volume=0, trades=0.
	ok := f()
	ok.PnLMin = f64(0)
	ok.VolumeMin = f64(0)
	ok.TradeCountMin = i(0)
	ok.LastTradeAfter = "2026-09-01T00:00:00Z"
	ok.GroupID = "11111111-1111-1111-1111-111111111111"
	ok.SortBy = "last_trade"
	ok.SortDirection = "asc"
	ok.Limit = 100
	if err := ok.Validate(); err != nil {
		t.Errorf("boundary-valid request rejected: %v", err)
	}
}

// DISC-U-03: address normalization (spec §6: lowercase once, reject safely).
func TestNormalizeAddress(t *testing.T) {
	got, err := NormalizeAddress("  0xABCDEF0123456789abcdef0123456789ABCDEF01  ")
	if err != nil {
		t.Fatalf("valid address rejected: %v", err)
	}
	if got != "0xabcdef0123456789abcdef0123456789abcdef01" {
		t.Errorf("not lowercased: %q", got)
	}
	for _, bad := range []string{"", "0xZZZ", "abc", "0x1234", "0x" + string(make([]byte, 40))} {
		if _, err := NormalizeAddress(bad); mustCode(t, err) != domain.ErrCodeValidation {
			t.Errorf("%q: want validation error", bad)
		}
	}
}

// CUR-U-01: cursor roundtrip, tamper, foreign-query rejection.
func TestCursor_RoundtripAndTamper(t *testing.T) {
	secret := []byte("test-secret-for-cursor-codec")
	fp := "fingerprint-1"
	m := "12345.5"

	enc, err := EncodeCursor(secret, fp, &m, "0xabc")
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	gotM, gotA, err := DecodeCursor(secret, fp, enc)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if gotM == nil || *gotM != m || gotA != "0xabc" {
		t.Errorf("roundtrip mismatch: %v %q", gotM, gotA)
	}

	// NULL sort value roundtrips as nil.
	encNull, _ := EncodeCursor(secret, fp, nil, "0xabc")
	if gotM, _, err := DecodeCursor(secret, fp, encNull); err != nil || gotM != nil {
		t.Errorf("null value must roundtrip as nil: %v %v", gotM, err)
	}

	// Tampered payload / bad signature / foreign fingerprint / garbage.
	tampered := enc[:len(enc)-2] + "xx"
	for name, raw := range map[string]string{
		"tampered": tampered, "garbage": "not-a-cursor",
		"foreign fp": mustEncode(t, secret, "other-fp", &m, "0xabc"),
	} {
		if code := mustCode(t, func() error {
			_, _, err := DecodeCursor(secret, fp, raw)
			return err
		}()); code != domain.ErrCodeInvalidFilter {
			t.Errorf("%s: want INVALID_FILTER, got %s", name, code)
		}
	}
}

// MET-U-06: leaderboard window mapping for the ROI passthrough rule.
func TestLBWindowForPeriod(t *testing.T) {
	for period, window := range map[string]string{
		Period1D: "day", Period7D: "week", Period30D: "month", PeriodALL: "allTime",
	} {
		if LBWindowForPeriod[period] != window {
			t.Errorf("%s: want %s, got %s", period, window, LBWindowForPeriod[period])
		}
	}
}

func f64(v float64) *float64 { return &v }
func i(v int) *int           { return &v }

func mustEncode(t *testing.T, secret []byte, fp string, m *string, a string) string {
	t.Helper()
	s, err := EncodeCursor(secret, fp, m, a)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return s
}
