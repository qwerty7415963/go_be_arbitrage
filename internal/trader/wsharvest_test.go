package trader

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// WS-U-05: batch dedupe keeps one entry per address with max trade time.
func TestMergeBatch_Dedupe(t *testing.T) {
	base := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	dst := map[string]time.Time{}
	var events []WSTradeEvent
	for i := 0; i < 50; i++ {
		events = append(events, WSTradeEvent{
			Coin: "BTC", Time: base.Add(time.Duration(i) * time.Second),
			Buyer:  "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Seller: "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		})
	}
	merged, skipped := mergeBatch(dst, events)
	if len(dst) != 2 || merged != 100 || skipped != 0 {
		t.Errorf("dedupe: dst=%d merged=%d skipped=%d", len(dst), merged, skipped)
	}
	if !dst["0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"].Equal(base.Add(49 * time.Second)) {
		t.Errorf("max time kept: %v", dst)
	}
}

// WS-U-06: malformed entries skipped safely and counted.
func TestMergeBatch_SkipsBad(t *testing.T) {
	dst := map[string]time.Time{}
	events := []WSTradeEvent{
		{Coin: "BTC", Time: time.Now().UTC(), Buyer: "zzz", Seller: ""},
		{Coin: "BTC", Time: time.Now().UTC(), Buyer: "0xcccccccccccccccccccccccccccccccccccccccc", Seller: "nope"},
		{},
	}
	merged, skipped := mergeBatch(dst, events)
	if len(dst) != 1 || merged != 1 || skipped != 5 {
		t.Errorf("skips: dst=%d merged=%d skipped=%d", len(dst), merged, skipped)
	}
}

// BE-036 (unit half): a full bounded queue drops incoming batches loudly
// instead of growing memory.
func TestWSHarvest_SubmitDropsWhenFull(t *testing.T) {
	svc := NewWSHarvestService(nil, uuid.Nil, 500, time.Hour)
	addr := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	for i := 0; i < 5000; i++ {
		svc.Submit([]WSTradeEvent{{Coin: "BTC", Time: time.Now().UTC(), Buyer: addr, Seller: addr}})
	}
	if got := svc.Stats().Dropped; got != 5000-4096 {
		t.Errorf("drops: want 904, got %d", got)
	}
}
