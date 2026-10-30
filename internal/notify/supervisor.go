package notify

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/qwerty7415963/go_be_arbitrage/internal/trader"
)

// WatcherManager is the subset of trader.WatcherManager used by the supervisor.
type WatcherManager interface {
	Retain(addr string) error
	Release(addr string)
}

// WalletSource provides the desired set of watched wallet addresses.
type WalletSource interface {
	WatchedWallets(ctx context.Context) ([]string, error)
}

type Supervisor struct {
	wm       WatcherManager
	src      WalletSource
	interval time.Duration

	mu       sync.Mutex
	retained map[string]struct{}
}

func NewSupervisor(args ...interface{}) *Supervisor {
	s := &Supervisor{
		interval: 30 * time.Second,
		retained: map[string]struct{}{},
	}
	for _, a := range args {
		switch v := a.(type) {
		case WatcherManager:
			s.wm = v
		case *trader.WatcherManager:
			if v != nil {
				s.wm = v
			}
		case WalletSource:
			s.src = v
		case *Repository:
			if v != nil {
				s.src = v
			}
		case time.Duration:
			if v > 0 {
				s.interval = v
			}
		}
	}
	if s.interval <= 0 {
		s.interval = 30 * time.Second
	}
	return s
}

func (s *Supervisor) Reconcile(ctx context.Context) error {
	if s == nil {
		return nil
	}
	var wallets []string
	if s.src != nil {
		ws, err := s.src.WatchedWallets(ctx)
		if err != nil {
			return err
		}
		wallets = ws
	}
	desired := map[string]struct{}{}
	for _, w := range wallets {
		desired[strings.ToLower(strings.TrimSpace(w))] = struct{}{}
	}

	s.mu.Lock()
	current := s.retained
	toRetain := []string{}
	toRelease := []string{}
	for w := range desired {
		if _, ok := current[w]; !ok {
			toRetain = append(toRetain, w)
		}
	}
	for w := range current {
		if _, ok := desired[w]; !ok {
			toRelease = append(toRelease, w)
		}
	}
	s.retained = desired
	s.mu.Unlock()

	if s.wm != nil {
		for _, w := range toRetain {
			_ = s.wm.Retain(w)
		}
		for _, w := range toRelease {
			s.wm.Release(w)
		}
	}
	return nil
}

// aliases
func (s *Supervisor) ReconcileOnce(ctx context.Context) error { return s.Reconcile(ctx) }
func (s *Supervisor) Tick(ctx context.Context) error          { return s.Reconcile(ctx) }
func (s *Supervisor) RunOnce(ctx context.Context) error       { return s.Reconcile(ctx) }

func (s *Supervisor) Retained() map[string]struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]struct{}{}
	for k := range s.retained {
		out[k] = struct{}{}
	}
	return out
}

func (s *Supervisor) Start(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	_ = s.Reconcile(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = s.Reconcile(ctx)
		}
	}
}
