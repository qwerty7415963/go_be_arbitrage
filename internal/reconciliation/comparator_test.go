package reconciliation

import (
	"testing"
)

func TestCompareBalance_Match(t *testing.T) {
	c := NewComparator()
	internal := &BalanceState{Asset: "BTC", Amount: 1.5, Reserved: 0.1}
	external := &BalanceState{Asset: "BTC", Amount: 1.5, Reserved: 0.1}
	item := c.CompareBalance(internal, external)
	if item.Result != ResultMatch {
		t.Errorf("expected MATCH, got %s", item.Result)
	}
}

func TestCompareBalance_Mismatch(t *testing.T) {
	c := NewComparator()
	internal := &BalanceState{Asset: "BTC", Amount: 1.5, Reserved: 0.1}
	external := &BalanceState{Asset: "BTC", Amount: 1.0, Reserved: 0.1}
	item := c.CompareBalance(internal, external)
	if item.Result != ResultMismatch {
		t.Errorf("expected MISMATCH, got %s", item.Result)
	}
}

func TestCompareBalance_MissingInternal(t *testing.T) {
	c := NewComparator()
	external := &BalanceState{Asset: "BTC", Amount: 1.5, Reserved: 0.1}
	item := c.CompareBalance(nil, external)
	if item.Result != ResultMissingInternal {
		t.Errorf("expected MISSING_INTERNAL, got %s", item.Result)
	}
}

func TestCompareBalance_MissingExternal(t *testing.T) {
	c := NewComparator()
	internal := &BalanceState{Asset: "BTC", Amount: 1.5, Reserved: 0.1}
	item := c.CompareBalance(internal, nil)
	if item.Result != ResultMissingExternal {
		t.Errorf("expected MISSING_EXTERNAL, got %s", item.Result)
	}
}

func TestComparePosition_Match(t *testing.T) {
	c := NewComparator()
	internal := &PositionState{Symbol: "BTCUSDT", Side: "LONG", Quantity: 0.5, AvgPrice: 50000}
	external := &PositionState{Symbol: "BTCUSDT", Side: "LONG", Quantity: 0.5, AvgPrice: 50000}
	item := c.ComparePosition(internal, external)
	if item.Result != ResultMatch {
		t.Errorf("expected MATCH, got %s", item.Result)
	}
}

func TestComparePosition_Mismatch(t *testing.T) {
	c := NewComparator()
	internal := &PositionState{Symbol: "BTCUSDT", Side: "LONG", Quantity: 0.5, AvgPrice: 50000}
	external := &PositionState{Symbol: "BTCUSDT", Side: "SHORT", Quantity: 0.3, AvgPrice: 51000}
	item := c.ComparePosition(internal, external)
	if item.Result != ResultMismatch {
		t.Errorf("expected MISMATCH, got %s", item.Result)
	}
}

func TestCompareOrder_Match(t *testing.T) {
	c := NewComparator()
	internal := &OrderState{OrderID: "ord-1", Symbol: "BTCUSDT", Side: "BUY", Quantity: 0.1, Status: "OPEN", FilledQty: 0}
	external := &OrderState{OrderID: "ord-1", Symbol: "BTCUSDT", Side: "BUY", Quantity: 0.1, Status: "OPEN", FilledQty: 0}
	item := c.CompareOrder(internal, external)
	if item.Result != ResultMatch {
		t.Errorf("expected MATCH, got %s", item.Result)
	}
}

func TestCompareOrder_MissingExternal(t *testing.T) {
	c := NewComparator()
	internal := &OrderState{OrderID: "ord-1", Symbol: "BTCUSDT", Side: "BUY", Quantity: 0.1, Status: "OPEN"}
	item := c.CompareOrder(internal, nil)
	if item.Result != ResultMissingExternal {
		t.Errorf("expected MISSING_EXTERNAL, got %s", item.Result)
	}
}

func TestCompareOrder_MissingInternal(t *testing.T) {
	c := NewComparator()
	external := &OrderState{OrderID: "ord-1", Symbol: "BTCUSDT", Side: "BUY", Quantity: 0.1, Status: "OPEN"}
	item := c.CompareOrder(nil, external)
	if item.Result != ResultMissingInternal {
		t.Errorf("expected MISSING_INTERNAL, got %s", item.Result)
	}
}

func TestCompareMargin_Match(t *testing.T) {
	c := NewComparator()
	internal := &MarginState{TotalEquity: 10000, UsedMargin: 2000, AvailableMargin: 8000, MarginRatio: 0.2}
	external := &MarginState{TotalEquity: 10000, UsedMargin: 2000, AvailableMargin: 8000, MarginRatio: 0.2}
	item := c.CompareMargin(internal, external)
	if item.Result != ResultMatch {
		t.Errorf("expected MATCH, got %s", item.Result)
	}
}

func TestCompareMargin_Mismatch(t *testing.T) {
	c := NewComparator()
	internal := &MarginState{TotalEquity: 10000, UsedMargin: 2000, AvailableMargin: 8000, MarginRatio: 0.2}
	external := &MarginState{TotalEquity: 10000, UsedMargin: 3000, AvailableMargin: 7000, MarginRatio: 0.3}
	item := c.CompareMargin(internal, external)
	if item.Result != ResultMismatch {
		t.Errorf("expected MISMATCH, got %s", item.Result)
	}
}

func TestCompareFills_Match(t *testing.T) {
	c := NewComparator()
	internal := []*FillState{
		{FillID: "f1", OrderID: "o1", Quantity: 0.1, Price: 50000},
	}
	external := []*FillState{
		{FillID: "f1", OrderID: "o1", Quantity: 0.1, Price: 50000},
	}
	item := c.CompareFills(internal, external)
	if item.Result != ResultMatch {
		t.Errorf("expected MATCH, got %s", item.Result)
	}
}

func TestCompareFills_Mismatch(t *testing.T) {
	c := NewComparator()
	internal := []*FillState{
		{FillID: "f1", OrderID: "o1", Quantity: 0.1, Price: 50000},
	}
	external := []*FillState{
		{FillID: "f1", OrderID: "o1", Quantity: 0.2, Price: 51000},
	}
	item := c.CompareFills(internal, external)
	if item.Result != ResultMismatch {
		t.Errorf("expected MISMATCH, got %s", item.Result)
	}
}

func TestSummarizeItems(t *testing.T) {
	c := NewComparator()
	items := []*ReconciliationItem{
		{Result: ResultMatch},
		{Result: ResultMatch},
		{Result: ResultMismatch},
		{Result: ResultMissingInternal},
	}
	summary := c.SummarizeItems(items)
	if summary.TotalItems != 4 {
		t.Errorf("expected 4 total items, got %d", summary.TotalItems)
	}
	if summary.Matched != 2 {
		t.Errorf("expected 2 matched, got %d", summary.Matched)
	}
	if summary.Mismatched != 1 {
		t.Errorf("expected 1 mismatched, got %d", summary.Mismatched)
	}
	if summary.MissingInt != 1 {
		t.Errorf("expected 1 missing internal, got %d", summary.MissingInt)
	}
}

func TestOverallStatus_AllMatch(t *testing.T) {
	c := NewComparator()
	summary := &RunSummary{TotalItems: 3, Matched: 3}
	status := c.OverallStatus(summary)
	if status != RunStatusMatched {
		t.Errorf("expected MATCHED, got %s", status)
	}
}

func TestOverallStatus_SomeMismatch(t *testing.T) {
	c := NewComparator()
	summary := &RunSummary{TotalItems: 3, Matched: 2, Mismatched: 1}
	status := c.OverallStatus(summary)
	if status != RunStatusMismatch {
		t.Errorf("expected MISMATCH, got %s", status)
	}
}

func TestBalanceBothNil(t *testing.T) {
	c := NewComparator()
	item := c.CompareBalance(nil, nil)
	if item.Result != ResultMatch {
		t.Errorf("expected MATCH for both nil, got %s", item.Result)
	}
}

func TestPositionBothNil(t *testing.T) {
	c := NewComparator()
	item := c.ComparePosition(nil, nil)
	if item.Result != ResultMatch {
		t.Errorf("expected MATCH for both nil, got %s", item.Result)
	}
}

func TestMarginBothNil(t *testing.T) {
	c := NewComparator()
	item := c.CompareMargin(nil, nil)
	if item.Result != ResultMatch {
		t.Errorf("expected MATCH for both nil, got %s", item.Result)
	}
}
