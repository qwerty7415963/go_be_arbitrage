package hyperliquid

import (
	"context"
	"sync"
	"time"
)

// AdaptivePacer spaces upstream calls with a floor interval that doubles on
// rate-limit signals (up to a cap) and decays back on success streaks
// (spec: retries respect backoff; no hot-looping a struggling upstream).
type AdaptivePacer struct {
	mu      sync.Mutex
	base    time.Duration
	current time.Duration
	max     time.Duration
	streak  int
	last    time.Time
}

func NewAdaptivePacer(base, max time.Duration) *AdaptivePacer {
	if max <= 0 {
		max = 30 * time.Second
	}
	if base < 0 {
		base = 0
	}
	return &AdaptivePacer{base: base, current: base, max: max}
}

// Wait sleeps until the current interval elapsed since the last call.
// Context cancellation aborts the wait.
func (p *AdaptivePacer) Wait(ctx context.Context) error {
	p.mu.Lock()
	wait := p.current - time.Since(p.last)
	p.mu.Unlock()
	if wait <= 0 {
		p.mark()
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		p.mark()
		return nil
	}
}

func (p *AdaptivePacer) mark() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.last = time.Now()
}

// Backoff doubles the interval up to the cap and resets the success streak.
// The floor is the configured base (a zero base steps to one second so even
// unpaced clients slow down on 429).
func (p *AdaptivePacer) Backoff() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.current *= 2
	if p.base == 0 && p.current < time.Second {
		p.current = time.Second
	}
	if p.current > p.max {
		p.current = p.max
	}
	if p.current < p.base {
		p.current = p.base
	}
	p.streak = 0
}

// Success records a clean call; every 5 in a row halve the interval toward
// the floor.
func (p *AdaptivePacer) Success() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.streak++
	if p.streak >= 5 {
		p.streak = 0
		p.current /= 2
		if p.current < p.base {
			p.current = p.base
		}
	}
}

// Interval reports the current spacing (tests + observability).
func (p *AdaptivePacer) Interval() time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.current
}
