package notify

import (
	"testing"
)

func TestCoalescing(t *testing.T) {
	c := NewCoalescer(3 * 1e9) // 3s in nanoseconds? time.Duration
	_ = c
	// Use seconds duration
	c = NewCoalescer(3 * 1000000000)
	for i := 0; i < 3; i++ {
		it := Intent{
			Kind:     "position_opened",
			DedupKey: "fill:1",
			Wallet:   "0xabc",
			Data: map[string]any{
				"coin": "BTC", "dir": "Open Long", "size": 1.0, "price": 100.0 + float64(i),
			},
		}
		// distinct tid for dedup not relevant
		it.DedupKey = "fill:" + string(rune('1'+i))
		c.Add(it)
	}
	// force flush
	out := c.Flush()
	if len(out) != 1 {
		t.Fatalf("expected 1 coalesced intent, got %d", len(out))
	}
	if sz := toFloatAny(out[0].Data["size"]); sz != 3 {
		t.Fatalf("size=%v", sz)
	}
}
