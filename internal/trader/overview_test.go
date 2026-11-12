package trader

import (
	"testing"
	"time"
)

func TestPnLBucketFor(t *testing.T) {
	cases := []struct {
		v    float64
		want string
	}{
		{1000000, "extremely_profitable"},
		{2000000, "extremely_profitable"},
		{100001, "very_profitable"},
		{999999, "very_profitable"},
		{100000, "profitable"},
		{99999, "profitable"},
		{1, "profitable"},
		{0, ""},
		{-1, "unprofitable"},
		{-100000, "unprofitable"},
		{-100001, "very_unprofitable"},
		{-1000000, "rekt"},
		{-1000001, "rekt"},
	}
	for _, c := range cases {
		if got := PnLBucketFor(c.v); got != c.want {
			t.Errorf("PnLBucketFor(%v)=%s want %s", c.v, got, c.want)
		}
	}
}

func TestAccountBucketFor(t *testing.T) {
	cases := []struct {
		v    float64
		want string
	}{
		{5000000, "apex"},
		{1000000, "whale"},
		{100000, "large"},
		{10000, "medium"},
		{0, "small"},
		{9999, "small"},
	}
	for _, c := range cases {
		if got := AccountBucketFor(c.v); got != c.want {
			t.Errorf("AccountBucketFor(%v)=%s want %s", c.v, got, c.want)
		}
	}
}

func TestSentiment(t *testing.T) {
	if got := Sentiment(0, 0); got != "neutral" {
		t.Errorf("zero total: %s", got)
	}
	cases := []struct {
		l, s int64
		want string
	}{
		{10, 0, "very_bullish"},
		{6, 4, "bullish"},
		{53, 47, "slightly_bullish"},
		{50, 50, "neutral"},
		{46, 54, "slightly_bearish"},
		{4, 6, "bearish"},
		{1, 9, "very_bearish"},
	}
	for _, c := range cases {
		if got := Sentiment(c.l, c.s); got != c.want {
			t.Errorf("Sentiment(%d,%d)=%s want %s", c.l, c.s, got, c.want)
		}
	}
}

func TestLongRatio(t *testing.T) {
	if LongRatio(3, 1) != 0.75 {
		t.Errorf("ratio")
	}
	if LongRatio(0, 0) != 0 {
		t.Errorf("zero")
	}
}

func TestApproxNotional(t *testing.T) {
	l, s := ApproxNotional(100, 3, 1)
	if l != 75 || s != 25 {
		t.Errorf("got %v %v", l, s)
	}
	l2, s2 := ApproxNotional(100, 0, 0)
	if l2 != 0 || s2 != 0 {
		t.Errorf("zero")
	}
}

func TestCumulativeSeries(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
	daily := map[time.Time]float64{
		start:                     10,
		start.Add(48 * time.Hour): 5,
	}
	pts := CumulativeSeries(daily, start, end)
	if len(pts) != 3 {
		t.Fatalf("len %d", len(pts))
	}
	if pts[0].V != 10 {
		t.Errorf("d0 %v", pts[0].V)
	}
	if pts[1].V != 10 {
		t.Errorf("d1 carry %v", pts[1].V)
	}
	if pts[2].V != 15 {
		t.Errorf("d2 %v", pts[2].V)
	}
}
func TestOverviewAccountBucketSentiment(t *testing.T) {
	cases := []struct {
		l, s int64
		want string
	}{
		{6, 4, "bullish"},
		{2, 3, "bearish"},
		{0, 0, "neutral"},
	}
	for _, c := range cases {
		if got := Sentiment(c.l, c.s); got != c.want {
			t.Errorf("account bucket Sentiment(%d,%d)=%s want %s", c.l, c.s, got, c.want)
		}
	}
}

func TestPnLBucketForZeroExcluded(t *testing.T) {
	if got := PnLBucketFor(0); got != "" {
		t.Errorf("PnLBucketFor(0)=%s want empty", got)
	}
}

func TestPnLBucketForAllZeroEmpty(t *testing.T) {
	vals := []float64{0, 0, 0}
	for _, v := range vals {
		if got := PnLBucketFor(v); got != "" {
			t.Errorf("PnLBucketFor(%v)=%s want empty", v, got)
		}
	}
}

func TestValidBucketID(t *testing.T) {
	validPnl := []string{"extremely_profitable", "very_profitable", "profitable", "unprofitable", "very_unprofitable", "rekt"}
	for _, id := range validPnl {
		if !ValidBucketID("pnl", id) {
			t.Errorf("ValidBucketID(pnl,%s)=false want true", id)
		}
	}
	validAcct := []string{"apex", "whale", "large", "medium", "small"}
	for _, id := range validAcct {
		if !ValidBucketID("account", id) {
			t.Errorf("ValidBucketID(account,%s)=false want true", id)
		}
	}
	invalid := []struct {
		kind, id string
	}{
		{"pnl", "apex"}, {"account", "profitable"}, {"pnl", ""}, {"account", ""}, {"foo", "profitable"}, {"", "profitable"}, {"pnl", "bogus"}, {"account", "bogus"},
	}
	for _, c := range invalid {
		if ValidBucketID(c.kind, c.id) {
			t.Errorf("ValidBucketID(%q,%q)=true want false", c.kind, c.id)
		}
	}
}

func TestRangeDays(t *testing.T) {
	cases := []struct {
		in   string
		want int
		ok   bool
	}{
		{"24H", 1, true},
		{"7D", 7, true},
		{"30D", 30, true},
		{"1H", 0, false},
		{"", 0, false},
		{"bogus", 0, false},
		{"24h", 0, false},
	}
	for _, c := range cases {
		got, err := rangeDays(c.in)
		if c.ok {
			if err != nil || got != c.want {
				t.Errorf("rangeDays(%q)=%d,%v want %d,nil", c.in, got, err, c.want)
			}
		} else {
			if err == nil {
				t.Errorf("rangeDays(%q)=%d,nil want err", c.in, got)
			}
		}
	}
}
