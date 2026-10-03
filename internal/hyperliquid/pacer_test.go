package hyperliquid

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// SYNC-U-02: backoff doubles within bounds; success streaks decay to floor.
func TestAdaptivePacer(t *testing.T) {
	p := NewAdaptivePacer(10*time.Millisecond, 40*time.Millisecond)
	if got := p.Interval(); got != 10*time.Millisecond {
		t.Fatalf("floor: %v", got)
	}
	p.Backoff()
	p.Backoff()
	if got := p.Interval(); got != 40*time.Millisecond {
		t.Errorf("doubling capped: %v", got)
	}
	p.Backoff()
	if got := p.Interval(); got != 40*time.Millisecond {
		t.Errorf("cap holds: %v", got)
	}
	for i := 0; i < 4; i++ {
		p.Success()
	}
	if got := p.Interval(); got != 40*time.Millisecond {
		t.Errorf("no decay before streak of 5: %v", got)
	}
	p.Success()
	if got := p.Interval(); got != 20*time.Millisecond {
		t.Errorf("decay after 5: %v", got)
	}
	for i := 0; i < 10; i++ {
		p.Success()
	}
	if got := p.Interval(); got != 10*time.Millisecond {
		t.Errorf("floor: %v", got)
	}
	if err := p.Wait(context.Background()); err != nil {
		t.Errorf("wait: %v", err)
	}

	// Disabled pacer (base 0) never sleeps.
	q := NewAdaptivePacer(0, 0)
	start := time.Now()
	if err := q.Wait(context.Background()); err != nil || time.Since(start) > time.Second {
		t.Errorf("disabled pacer must not sleep: %v", err)
	}
	// ...unless it backs off: zero steps to one second, capped by max.
	q.Backoff()
	if got := q.Interval(); got != time.Second {
		t.Errorf("zero-base backoff: %v", got)
	}
}

// SYNC-I-05: upstream 429×2 then OK — gaps grow via backoff, then success.
func TestPacer_429Recovery(t *testing.T) {
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) <= 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()
	c := NewClient(srv.URL, 5*time.Second, 5*time.Millisecond)

	start := time.Now()
	for i := 0; i < 3; i++ {
		fills, err := c.FetchWindow(context.Background(), "0xabc", 1, 2)
		if i < 2 {
			if err == nil {
				t.Fatalf("call %d must 429", i)
			}
			continue
		}
		if err != nil {
			t.Fatalf("third call must succeed: %v", err)
		}
		if len(fills) != 0 {
			t.Errorf("fills: %+v", fills)
		}
	}
	if elapsed := time.Since(start); elapsed < 25*time.Millisecond {
		t.Errorf("backoff gaps not observed: %v", elapsed)
	}
	if got := c.pacer.Interval(); got != 20*time.Millisecond {
		t.Errorf("interval after 429,429,ok: %v", got)
	}
}
