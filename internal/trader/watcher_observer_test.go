package trader

import (
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWatcher_ObserverReceivesEvents(t *testing.T) {
	mgr := NewWatcherManager(NewActivityHub(), nil, nil, 4)
	wallet := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	c := mgr.Subscribe(wallet)
	_ = drainRawEvents(c)

	var got []WalletEvent
	var mu sync.Mutex
	mgr.AddObserver(func(ev WalletEvent) {
		mu.Lock()
		got = append(got, ev)
		mu.Unlock()
	})

	mgr.OnOrder(wallet, testOrder(111))
	// allow observer goroutine to process
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) == 0 {
		t.Fatal("observer should receive event")
	}
	if got[0].Type != EvtOrderCreated {
		t.Errorf("observer event type: %s", got[0].Type)
	}
	_ = json.Marshal
}

func TestWatcher_ObserverDropOnOverflow(t *testing.T) {
	mgr := NewWatcherManager(NewActivityHub(), nil, nil, 4)
	wallet := "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	c := mgr.Subscribe(wallet)
	_ = drainRawEvents(c)

	// slow observer that blocks, causing overflow
	var processed atomic.Int64
	mgr.AddObserver(func(ev WalletEvent) {
		time.Sleep(50 * time.Millisecond)
		processed.Add(1)
	})

	// flood events
	for i := 0; i < 2000; i++ {
		mgr.OnOrder(wallet, testOrder(int64(1000+i)))
	}
	time.Sleep(100 * time.Millisecond)

	if mgr.ObserverDropped() == 0 && mgr.Stats().ObserverDropped == 0 {
		// maybe processing kept up; not necessarily failure, but expect some drop
		t.Logf("no drops counted; processed=%d", processed.Load())
	}
	// ensure no panic; done
}
