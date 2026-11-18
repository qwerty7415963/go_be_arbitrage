package trader

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/qwerty7415963/go_be_arbitrage/internal/hyperliquid"
)

func TestWatcher_PositionRefresh_Debounce(t *testing.T) {
	mgr := NewWatcherManager(NewActivityHub(), nil, nil, 4)
	wallet := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	c := mgr.Subscribe(wallet)
	_ = drainRawEvents(c)

	var calls int32
	mgr.WithPositionProvider(func(ctx context.Context, w string) (*PositionSnapshotDTO, error) {
		atomic.AddInt32(&calls, 1)
		return &PositionSnapshotDTO{Positions: []PositionDTO{}, DataStatus: DataReady}, nil
	})

	mgr.OnFill(wallet, FillDTO{Coin: "BTC", Side: "BUY", Size: 1, Price: 1, Time: time.Now().UTC(), Tid: 1})
	mgr.OnFill(wallet, FillDTO{Coin: "BTC", Side: "BUY", Size: 1, Price: 1, Time: time.Now().UTC(), Tid: 2})

	time.Sleep(2500 * time.Millisecond)

	evs := drainRawEvents(c)
	cnt := 0
	for _, e := range evs {
		if e.Type == EvtPositionUpdated {
			cnt++
		}
	}
	if cnt != 1 {
		t.Fatalf("want 1 position.updated after debounce, got %d (%+v)", cnt, evs)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("want provider called once, got %d", calls)
	}
}

func TestWatcher_PositionRefresh_GateFullSkip(t *testing.T) {
	mgr := NewWatcherManager(NewActivityHub(), nil, nil, 1)
	wallet := "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	c := mgr.Subscribe(wallet)
	_ = drainRawEvents(c)

	// Hold the single gate slot.
	mgr.gate <- struct{}{}

	var calls int32
	mgr.WithPositionProvider(func(ctx context.Context, w string) (*PositionSnapshotDTO, error) {
		atomic.AddInt32(&calls, 1)
		return &PositionSnapshotDTO{Positions: []PositionDTO{}, DataStatus: DataReady}, nil
	})

	mgr.OnFill(wallet, FillDTO{Coin: "BTC", Side: "BUY", Size: 1, Price: 1, Time: time.Now().UTC(), Tid: 3})

	time.Sleep(2100 * time.Millisecond)

	if mgr.Stats().PositionRefreshDropped < 1 {
		t.Fatalf("want dropped >=1, got %d", mgr.Stats().PositionRefreshDropped)
	}
	if atomic.LoadInt32(&calls) != 0 {
		t.Fatalf("provider should not be called when gate full, got %d", calls)
	}

	// Release gate.
	<-mgr.gate
}

func TestWatcher_PositionRefresh_ErrorKeepsLast(t *testing.T) {
	mgr := NewWatcherManager(NewActivityHub(), nil, nil, 4)
	wallet := "0xcccccccccccccccccccccccccccccccccccccccc"
	c := mgr.Subscribe(wallet)
	_ = drainRawEvents(c)

	last := &PositionSnapshotDTO{Positions: []PositionDTO{}, DataStatus: DataReady, AsOf: nil}
	var mode int32
	mgr.WithPositionProvider(func(ctx context.Context, w string) (*PositionSnapshotDTO, error) {
		if atomic.LoadInt32(&mode) == 0 {
			return last, nil
		}
		return nil, errors.New("boom")
	})

	// First refresh succeeds and sets last.
	mgr.OnFill(wallet, FillDTO{Coin: "BTC", Side: "BUY", Size: 1, Price: 1, Time: time.Now().UTC(), Tid: 4})
	time.Sleep(2500 * time.Millisecond)
	_ = drainRawEvents(c)

	// Second refresh fails; no emit, last retained.
	atomic.StoreInt32(&mode, 1)
	mgr.OnFill(wallet, FillDTO{Coin: "BTC", Side: "BUY", Size: 1, Price: 1, Time: time.Now().UTC(), Tid: 5})
	time.Sleep(2500 * time.Millisecond)

	evs := drainRawEvents(c)
	cnt := 0
	for _, e := range evs {
		if e.Type == EvtPositionUpdated {
			cnt++
		}
	}
	if cnt != 0 {
		t.Fatalf("want no position.updated on error, got %d", cnt)
	}
	if mgr.Stats().PositionRefreshErrors < 1 {
		t.Fatalf("want error counter >=1")
	}

	mgr.mu.Lock()
	w, _ := mgr.watchers[wallet]
	w.mu.Lock()
	got := w.lastPositionDTO
	w.mu.Unlock()
	mgr.mu.Unlock()
	if got != last {
		t.Fatalf("last DTO not retained")
	}
}

func TestWatcher_PositionUpdated_BootstrapFullSnapshot(t *testing.T) {
	mgr := NewWatcherManager(NewActivityHub(), nil, nil, 4)
	var providerCalled bool
	mgr.WithPositionProvider(func(ctx context.Context, w string) (*PositionSnapshotDTO, error) {
		providerCalled = true
		return &PositionSnapshotDTO{Positions: []PositionDTO{}, DataStatus: DataReady}, nil
	})

	wallet := "0xdddddddddddddddddddddddddddddddddddddddd"
	c := mgr.Subscribe(wallet)
	evs := drainRawEvents(c)

	found := false
	for _, e := range evs {
		if e.Type == EvtPositionUpdated {
			var raw map[string]any
			if err := json.Unmarshal(e.Data, &raw); err == nil {
				if _, ok := raw["positions"]; ok {
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatalf("bootstrap must emit full position snapshot, got %+v", evs)
	}
	if !providerCalled {
		t.Fatal("provider should be called on bootstrap")
	}
	_ = c
}

func TestWatcher_Teardown_StopsPositionTimer(t *testing.T) {
	mgr := NewWatcherManager(NewActivityHub(), nil, nil, 4)
	wallet := "0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	c := mgr.Subscribe(wallet)
	_ = drainRawEvents(c)

	mgr.WithPositionProvider(func(ctx context.Context, w string) (*PositionSnapshotDTO, error) {
		return &PositionSnapshotDTO{Positions: []PositionDTO{}, DataStatus: DataReady}, nil
	})

	mgr.OnFill(wallet, FillDTO{Coin: "BTC", Side: "BUY", Size: 1, Price: 1, Time: time.Now().UTC(), Tid: 6})
	mgr.Unsubscribe(c)

	time.Sleep(2500 * time.Millisecond)

	if mgr.Stats().PositionRefreshes != 0 {
		t.Fatalf("want no refresh after teardown, got %d", mgr.Stats().PositionRefreshes)
	}
	_ = hyperliquid.Fill{}
}
