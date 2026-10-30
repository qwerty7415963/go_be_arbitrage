package notify

import (
	"context"
	"sync"
	"testing"
)

type fakeWatcher struct {
	mu      sync.Mutex
	retain  []string
	release []string
}

func (f *fakeWatcher) Retain(addr string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.retain = append(f.retain, addr)
	return nil
}
func (f *fakeWatcher) Release(addr string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.release = append(f.release, addr)
}

type fakeSource struct {
	wallets []string
}

func (f *fakeSource) WatchedWallets(ctx context.Context) ([]string, error) {
	return f.wallets, nil
}

func TestSupervisorDiff(t *testing.T) {
	wm := &fakeWatcher{}
	src := &fakeSource{wallets: []string{"0xaaa"}}
	sup := NewSupervisor(wm, src)
	ctx := context.Background()
	if err := sup.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if len(wm.retain) != 1 || wm.retain[0] != "0xaaa" {
		t.Fatalf("retain=%v", wm.retain)
	}
	src.wallets = []string{}
	if err := sup.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if len(wm.release) != 1 || wm.release[0] != "0xaaa" {
		t.Fatalf("release=%v", wm.release)
	}
}
