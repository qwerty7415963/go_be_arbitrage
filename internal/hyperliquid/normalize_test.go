package hyperliquid

import (
	"testing"
	"time"
)

func fill(coin, dir, startPos, px, sz, closed, fee string, side string, ms int64, tid int64) Fill {
	return Fill{
		Coin: coin, Dir: dir, StartPosition: startPos,
		Px: px, Sz: sz, ClosedPnl: closed, Fee: fee,
		Side: side, Time: ms, Tid: tid, FeeToken: "USDC",
	}
}

func ms(s string) int64 {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t.UnixMilli()
}

// ING-U-01: valid fixture → MetricFill rows with time/qty/px, net PnL and
// leg PositionIDs.
func TestNormalizeFills_ValidFixture(t *testing.T) {
	raw := []Fill{
		fill("BTC", "Open Long", "0", "100", "1", "0", "0.1", "B", ms("2026-09-01T10:00:00Z"), 3),
		fill("BTC", "Close Long", "1", "110", "0.5", "5", "0.1", "A", ms("2026-09-01T11:00:00Z"), 4),
		fill("ETH", "Open Short", "0", "50", "2", "0", "0.2", "A", ms("2026-09-01T09:00:00Z"), 2),
	}

	got, skipped := NormalizeFills(raw)
	if skipped != 0 {
		t.Fatalf("expected 0 skipped, got %d", skipped)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(got))
	}

	// Oldest-first ordering.
	if !got[0].FilledAt.Before(got[1].FilledAt) || !got[1].FilledAt.Before(got[2].FilledAt) {
		t.Errorf("not oldest-first: %v %v %v", got[0].FilledAt, got[1].FilledAt, got[2].FilledAt)
	}

	eth := got[0]
	if eth.Market != "ETH" || eth.Side != "SHORT" || eth.Quantity != 2 || eth.Price != 50 {
		t.Errorf("ETH row: %+v", eth)
	}
	if eth.PositionID != "ETH#1" {
		t.Errorf("ETH leg: expected ETH#1, got %q", eth.PositionID)
	}

	open := got[1]
	if open.PositionID != "BTC#1" || open.Side != "LONG" {
		t.Errorf("BTC open: %+v", open)
	}
	if open.RealizedPnl != -0.1 { // 0 closedPnl − 0.1 fee
		t.Errorf("open net pnl: expected -0.1, got %v", open.RealizedPnl)
	}

	close := got[2]
	if close.PositionID != "BTC#1" { // partial close shares the leg
		t.Errorf("BTC close leg: expected BTC#1, got %q", close.PositionID)
	}
	if close.RealizedPnl != 5-0.1 { // net of fee
		t.Errorf("close net pnl: expected 4.9, got %v", close.RealizedPnl)
	}
	if close.ExchangeFillID == "" || close.Fee != 0.1 {
		t.Errorf("close row: %+v", close)
	}
}

// ING-U-02: bad decimals / unknown side / zero time → skipped, no crash,
// valid rows kept.
func TestNormalizeFills_BadRows_Skipped(t *testing.T) {
	raw := []Fill{
		fill("BTC", "Open Long", "0", "100", "1", "0", "0.1", "B", ms("2026-09-01T10:00:00Z"), 1),
		fill("BTC", "Open Long", "0", "abc", "1", "0", "0.1", "B", ms("2026-09-01T11:00:00Z"), 2),    // bad px
		fill("BTC", "Open Long", "0", "100", "1", "oops", "0.1", "B", ms("2026-09-01T12:00:00Z"), 3), // bad pnl
		fill("BTC", "???", "0", "100", "1", "0", "0.1", "X", ms("2026-09-01T13:00:00Z"), 4),          // unknown side
		fill("BTC", "Open Long", "0", "100", "1", "0", "0.1", "B", 0, 5),                             // zero time
		fill("BTC", "Open Long", "??", "100", "1", "0", "0.1", "B", ms("2026-09-01T14:00:00Z"), 6),   // bad startPosition
	}

	got, skipped := NormalizeFills(raw)
	if len(got) != 1 {
		t.Fatalf("expected 1 valid row, got %d", len(got))
	}
	if skipped != 5 {
		t.Errorf("expected 5 skipped, got %d", skipped)
	}
	if got[0].ExchangeFillID != "1" {
		t.Errorf("wrong row kept: %+v", got[0])
	}
}

// ING-U-03: partial closes share one leg; a later flat startPosition opens a
// new leg (new PositionID → engine counts 2 trades).
func TestNormalizeFills_LegSplitAfterFlat(t *testing.T) {
	raw := []Fill{
		fill("BTC", "Open Long", "0", "100", "2", "0", "0.1", "B", ms("2026-09-01T10:00:00Z"), 1),
		fill("BTC", "Close Long", "2", "110", "1", "10", "0.1", "A", ms("2026-09-01T11:00:00Z"), 2),
		fill("BTC", "Close Long", "1", "120", "1", "20", "0.1", "A", ms("2026-09-01T12:00:00Z"), 3),
		fill("BTC", "Open Short", "0", "120", "1", "0", "0.1", "A", ms("2026-09-01T13:00:00Z"), 4),
	}

	got, skipped := NormalizeFills(raw)
	if skipped != 0 || len(got) != 4 {
		t.Fatalf("got %d rows, %d skipped", len(got), skipped)
	}
	for i := 0; i < 3; i++ {
		if got[i].PositionID != "BTC#1" {
			t.Errorf("row %d: expected leg BTC#1, got %q", i, got[i].PositionID)
		}
	}
	if got[3].PositionID != "BTC#2" {
		t.Errorf("new leg: expected BTC#2, got %q", got[3].PositionID)
	}
	if got[3].Side != "SHORT" {
		t.Errorf("new leg side: expected SHORT, got %q", got[3].Side)
	}
}
