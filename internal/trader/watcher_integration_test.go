//go:build integration

package trader

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/qwerty7415963/go_be_arbitrage/internal/hyperliquid"
)

// watcherFakeLive feeds the watcher bootstrap (fills + open orders +
// funding) without touching the DB.
type watcherFakeLive struct {
	fills  []hyperliquid.Fill
	orders []hyperliquid.OpenOrder
}

func (f *watcherFakeLive) FetchClearinghouseState(_ context.Context, _ string) (*hyperliquid.ClearinghouseState, error) {
	return &hyperliquid.ClearinghouseState{}, nil
}

func (f *watcherFakeLive) FetchSpotState(_ context.Context, _ string) (*hyperliquid.SpotState, error) {
	return &hyperliquid.SpotState{}, nil
}

func (f *watcherFakeLive) FetchUserFills(_ context.Context, _ string) ([]hyperliquid.Fill, error) {
	return f.fills, nil
}

func (f *watcherFakeLive) FetchOpenOrders(_ context.Context, _ string) ([]hyperliquid.OpenOrder, error) {
	return f.orders, nil
}

func (f *watcherFakeLive) FetchHistoricalOrders(_ context.Context, _ string) ([]hyperliquid.HistoricalOrder, error) {
	return nil, nil
}

func (f *watcherFakeLive) FetchLedgerUpdates(_ context.Context, _ string, _ int64) ([]hyperliquid.LedgerUpdate, error) {
	return nil, nil
}

func (f *watcherFakeLive) FetchFillsWindow(_ context.Context, _ string, _, _ int64) ([]hyperliquid.Fill, bool, error) {
	return f.fills, false, nil
}

func (f *watcherFakeLive) FetchUserFunding(_ context.Context, _ string, _, _ int64) ([]hyperliquid.FundingUpdate, error) {
	return []hyperliquid.FundingUpdate{}, nil
}

func (f *watcherFakeLive) FetchPortfolio(_ context.Context, _ string) (map[string][]hyperliquid.PortfolioPoint, error) {
	return map[string][]hyperliquid.PortfolioPoint{}, nil
}

type countingUpstream struct {
	subs   atomic.Int64
	unsubs atomic.Int64
}

func (c *countingUpstream) Subscribe(_ context.Context, _ string) error {
	c.subs.Add(1)
	return nil
}

func (c *countingUpstream) Unsubscribe(_ string) error {
	c.unsubs.Add(1)
	return nil
}

// B1 integration: bootstrap-seeded oid emits updated (not created); a brand
// new oid emits created exactly once across 10 shared subscribers, later
// sightings stay updated.
func TestRepo_Watcher_OrderCreatedOnceShared(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	live := &watcherFakeLive{
		orders: []hyperliquid.OpenOrder{
			{Coin: "BTC", Side: "B", LimitPx: "50000", Sz: "1", OrigSz: "1",
				Oid: 9001, Timestamp: now.UnixMilli(), OrderType: "limit"},
		},
	}
	up := &countingUpstream{}
	mgr := NewWatcherManager(NewActivityHub(), live, up, 4)
	wallet := "0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"

	const subs = 10
	clients := make([]*ActivityClient, 0, subs)
	for i := 0; i < subs; i++ {
		clients = append(clients, mgr.Subscribe(wallet))
	}
	if got := mgr.WatcherCount(); got != 1 {
		t.Fatalf("10 subs must share exactly 1 watcher, got %d", got)
	}
	if got := up.subs.Load(); got != 1 {
		t.Fatalf("10 subs must cost 1 upstream subscribe, got %d", got)
	}
	snap := mgr.Snapshot(wallet)
	if _, ok := snap.Orders[9001]; !ok {
		t.Fatalf("bootstrap snapshot must hold seeded oid 9001, got %+v", snap.Orders)
	}
	for _, c := range clients {
		_ = drainRawEvents(c)
	}

	// Seeded oid was seen at bootstrap → updated, never created.
	mgr.OnOrder(wallet, testOrder(9001))
	for i, c := range clients {
		evs := drainRawEvents(c)
		if len(evs) != 1 || evs[0].Type != EvtOrderUpdated {
			t.Fatalf("client %d: seeded oid must emit 1x %s, got %+v", i, EvtOrderUpdated, evs)
		}
	}

	// Brand new oid → created exactly once on every subscriber.
	mgr.OnOrder(wallet, testOrder(9002))
	for i, c := range clients {
		evs := drainRawEvents(c)
		if len(evs) != 1 || evs[0].Type != EvtOrderCreated {
			t.Fatalf("client %d: new oid must emit 1x %s, got %+v", i, EvtOrderCreated, evs)
		}
		var created OrderCreatedPayload
		if err := json.Unmarshal(evs[0].Data, &created); err != nil {
			t.Fatalf("client %d: created decode: %v", i, err)
		}
		if created.Status != "open" || created.Oid != 9002 {
			t.Errorf("client %d: created payload: %+v", i, created)
		}
	}

	// Repeat → updated on all, created never repeats.
	mgr.OnOrder(wallet, testOrder(9002))
	for i, c := range clients {
		evs := drainRawEvents(c)
		if len(evs) != 1 || evs[0].Type != EvtOrderUpdated {
			t.Fatalf("client %d: repeat oid must emit 1x %s, got %+v", i, EvtOrderUpdated, evs)
		}
	}

	for _, c := range clients {
		mgr.Unsubscribe(c)
	}
	if got := mgr.WatcherCount(); got != 0 {
		t.Errorf("last unsub must tear down, watchers=%d", got)
	}
}

// B2 integration: streaming userFills row on the watcher path keeps dir +
// closedPnl end-to-end (subscribe → OnRawUserFill → wallet.fill.created).
func TestRepo_Watcher_FillCreatedDirEndToEnd(t *testing.T) {
	live := &watcherFakeLive{}
	mgr := NewWatcherManager(NewActivityHub(), live, nil, 4)
	wallet := "0xffffffffffffffffffffffffffffffffffffffff"
	c := mgr.Subscribe(wallet)
	_ = drainRawEvents(c)

	now := time.Now().UTC().Truncate(time.Millisecond).UnixMilli()
	if !mgr.OnRawUserFill(wallet, hyperliquid.Fill{
		Coin: "HYPE", Side: "A", Sz: "5", Px: "20", Time: now,
		Dir: "Open Short", ClosedPnl: "7.5", Fee: "0.2", FeeToken: "USDC",
		Tid: 77, Oid: 31, StartPosition: "0",
	}) {
		t.Fatal("valid row must emit")
	}
	evs := drainRawEvents(c)
	if len(evs) == 0 || evs[0].Type != EvtFillCreated {
		t.Fatalf("want %s first, got %+v", EvtFillCreated, evs)
	}
	var got FillDTO
	if err := json.Unmarshal(evs[0].Data, &got); err != nil {
		t.Fatalf("fill decode: %v", err)
	}
	if got.Dir != "Open Short" || got.ClosedPnl != 7.5 || got.Coin != "HYPE" {
		t.Errorf("watcher-path fill must keep dir/closedPnl, got %+v", got)
	}
	mgr.Unsubscribe(c)
}
