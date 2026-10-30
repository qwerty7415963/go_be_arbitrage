package notify

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// CoalesceKey identifies a coalescing bucket.
type CoalesceKey struct {
	Wallet string
	Coin   string
	Dir    string
}

// Coalescer collapses Intents sharing (wallet, coin, dir) within a window.
type Coalescer struct {
	window  time.Duration
	mu      sync.Mutex
	buckets map[string]*coalesceBucket
}

type coalesceBucket struct {
	key   CoalesceKey
	first time.Time
	items []Intent
}

func NewCoalescer(args ...interface{}) *Coalescer {
	w := 3 * time.Second
	for _, a := range args {
		switch v := a.(type) {
		case time.Duration:
			if v > 0 {
				w = v
			}
		}
	}
	return &Coalescer{window: w, buckets: map[string]*coalesceBucket{}}
}

func coalesceKeyOf(i Intent) CoalesceKey {
	wallet := i.Wallet
	coin := ""
	dir := ""
	if i.Data != nil {
		if v, ok := i.Data["coin"]; ok {
			coin = fmt.Sprintf("%v", v)
		}
		if v, ok := i.Data["dir"]; ok {
			dir = fmt.Sprintf("%v", v)
		}
		if dir == "" {
			if v, ok := i.Data["side"]; ok {
				dir = fmt.Sprintf("%v", v)
			}
		}
	}
	return CoalesceKey{Wallet: wallet, Coin: strings.ToUpper(strings.TrimSpace(coin)), Dir: strings.TrimSpace(dir)}
}

func (c *Coalescer) Add(i Intent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	k := coalesceKeyOf(i)
	bk := k.Wallet + "|" + k.Coin + "|" + k.Dir
	now := time.Now()
	b, ok := c.buckets[bk]
	if ok && now.Sub(b.first) < c.window {
		b.items = append(b.items, i)
		return
	}
	c.buckets[bk] = &coalesceBucket{key: k, first: now, items: []Intent{i}}
}

func (c *Coalescer) Flush() []Intent {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := []Intent{}
	for k, b := range c.buckets {
		if len(b.items) == 0 {
			continue
		}
		out = append(out, coalesceItems(b.key, b.items))
		delete(c.buckets, k)
	}
	return out
}

func (c *Coalescer) FlushExpired(now time.Time) []Intent {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := []Intent{}
	for k, b := range c.buckets {
		if len(b.items) == 0 {
			continue
		}
		if now.Sub(b.first) >= c.window {
			out = append(out, coalesceItems(b.key, b.items))
			delete(c.buckets, k)
		}
	}
	return out
}

func coalesceItems(k CoalesceKey, items []Intent) Intent {
	if len(items) == 0 {
		return Intent{}
	}
	if len(items) == 1 {
		return items[0]
	}
	totalSize := 0.0
	weightedPrice := 0.0
	totalPnL := 0.0
	totalFees := 0.0
	for _, it := range items {
		sz := toFloatAny(it.Data["size"])
		pr := toFloatAny(it.Data["price"])
		totalSize += sz
		weightedPrice += sz * pr
		totalPnL += toFloatAny(it.Data["closed_pnl"])
		if v := toFloatAny(it.Data["pnl"]); v != 0 && totalPnL == 0 {
			totalPnL += v
		}
		totalFees += toFloatAny(it.Data["fee"])
		if v := toFloatAny(it.Data["fees"]); v != 0 && totalFees == 0 {
			totalFees += v
		}
	}
	avgPrice := 0.0
	if totalSize != 0 {
		avgPrice = weightedPrice / totalSize
	}
	merged := items[0]
	if merged.Data == nil {
		merged.Data = map[string]any{}
	}
	merged.Data["size"] = totalSize
	merged.Data["price"] = avgPrice
	merged.Data["closed_pnl"] = totalPnL
	merged.Data["pnl"] = totalPnL
	merged.Data["net"] = totalPnL
	merged.Data["fee"] = totalFees
	merged.Data["fees"] = totalFees
	merged.DedupKey = items[0].DedupKey
	return merged
}

func toFloatAny(v any) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case float32:
		return float64(val)
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case string:
		var f float64
		fmt.Sscanf(val, "%f", &f)
		return f
	}
	return 0
}
