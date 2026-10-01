package trader

import (
	"testing"
)

// selectTop orders by month PnL desc (nil last), stable, capped at limit.
func TestSelectTop(t *testing.T) {
	rows := []DiscoveredWallet{
		{Address: "0x1", Windows: map[string]WindowStats{}},
		{Address: "0x2", Windows: map[string]WindowStats{"month": {PnL: f64(10)}}},
		{Address: "0x3", Windows: map[string]WindowStats{"month": {PnL: f64(900)}}},
		{Address: "0x4", Windows: map[string]WindowStats{"month": {PnL: f64(500)}}},
	}
	got := selectTop(rows, 2)
	if len(got) != 2 || got[0].Address != "0x3" || got[1].Address != "0x4" {
		t.Errorf("top-2: %+v", got)
	}
	all := selectTop(rows, 0)
	if len(all) != 4 || all[3].Address != "0x1" {
		t.Errorf("uncapped must keep all, nil last: %+v", all)
	}
	if len(rows) != 4 || rows[0].Address != "0x1" {
		t.Error("selectTop must not mutate its input")
	}
}
