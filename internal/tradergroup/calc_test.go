package tradergroup

import (
	"testing"
	"time"
)

func TestTWRChain(t *testing.T) {
	pts := []TWREquityPoint{
		{Start: 100, End: 110, Valid: true},
		{Start: 100, End: 90, Valid: true},
	}
	got := TWRChain(pts)
	if got == nil {
		t.Fatal("expected value")
	}
	// (1.1)*(0.9)-1 = -0.01
	if *got < -0.011 || *got > -0.009 {
		t.Errorf("got %v", *got)
	}
	// empty
	if TWRChain(nil) != nil {
		t.Error("empty should be nil")
	}
	// skip invalid
	pts2 := []TWREquityPoint{{Start: 0, End: 10, Valid: true}}
	if TWRChain(pts2) != nil {
		t.Error("zero start should skip -> nil")
	}
}

func TestMaxDrawdownPct(t *testing.T) {
	idx := []float64{100, 110, 99, 105}
	got := MaxDrawdownPct(idx)
	if got == nil {
		t.Fatal("expected")
	}
	// peak 110 -> trough 99 => 10%
	if *got < 9.9 || *got > 10.1 {
		t.Errorf("got %v", *got)
	}
	if MaxDrawdownPct(nil) != nil {
		t.Error("nil should be nil")
	}
}

func TestDeriveGroupStatus(t *testing.T) {
	if DeriveGroupStatus(0, 0, true, false) != "empty" {
		t.Error("empty")
	}
	if DeriveGroupStatus(2, 0, true, false) != "error" {
		t.Error("error")
	}
	if DeriveGroupStatus(2, 2, true, true) != "stale" {
		t.Error("stale")
	}
	if DeriveGroupStatus(2, 1, true, false) != "partial" {
		t.Error("partial")
	}
	if DeriveGroupStatus(2, 2, false, false) != "partial" {
		t.Error("partial ready false")
	}
	if DeriveGroupStatus(2, 2, true, false) != "ready" {
		t.Error("ready")
	}
}

func TestFavoriteOrdering(t *testing.T) {
	items := []*GroupListItem{
		{Name: "B", IsFavorite: false, AccountValue: ptr(10)},
		{Name: "A", IsFavorite: true, AccountValue: ptr(5)},
	}
	SortGroupListItems(items, "account_value", "asc")
	if !items[0].IsFavorite {
		t.Error("favorite should come first")
	}
	// tiebreak name
	items2 := []*GroupListItem{
		{Name: "Z", IsFavorite: false},
		{Name: "A", IsFavorite: false},
	}
	SortGroupListItems(items2, "name", "asc")
	if items2[0].Name != "A" {
		t.Error("name sort")
	}
	_ = time.Now()
}

func ptr(v float64) *float64 { return &v }

func TestWinRate(t *testing.T) {
	if WinRate(0, 0) != nil {
		t.Error("denom 0 should be nil")
	}
	got := WinRate(3, 1)
	if got == nil || *got < 0.74 || *got > 0.76 {
		t.Errorf("got %v", got)
	}
	// breakeven excluded
	got2 := WinRate(2, 2)
	if got2 == nil || *got2 != 0.5 {
		t.Errorf("got %v", got2)
	}
}

func TestProfitFactor(t *testing.T) {
	if ProfitFactor(10, 5, 0, 10) != nil {
		t.Error("no losses should be nil")
	}
	if ProfitFactor(10, 5, 2, 9) != nil {
		t.Error("trade_count<10 should be nil")
	}
	got := ProfitFactor(10, -5, 2, 10)
	if got == nil || *got < 1.99 || *got > 2.01 {
		t.Errorf("got %v", got)
	}
}
