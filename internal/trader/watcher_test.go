package trader

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/qwerty7415963/go_be_arbitrage/internal/hyperliquid"
)

// Helpers for watcher envelope tests (B1/B2): the manager emits
// synchronously into hub client channels (buffered 64), so draining right
// after On* calls is deterministic.

type rawWalletEvent struct {
	Type  string          `json:"type"`
	Data  json.RawMessage `json:"data"`
	State string          `json:"state"`
}

func drainRawEvents(c *ActivityClient) []rawWalletEvent {
	var out []rawWalletEvent
	for {
		select {
		case raw := <-c.send:
			var ev rawWalletEvent
			if err := json.Unmarshal(raw, &ev); err != nil {
				continue
			}
			out = append(out, ev)
		default:
			return out
		}
	}
}

func countTypes(evs []rawWalletEvent) map[string]int {
	m := map[string]int{}
	for _, e := range evs {
		m[e.Type]++
	}
	return m
}

func testOrder(oid int64) OrderDTO {
	return OrderDTO{
		Coin: "BTC", Side: "BUY", LimitPx: 50000, Size: 1, OrigSize: 1,
		Oid: oid, Timestamp: time.Now().UTC().Truncate(time.Millisecond),
		OrderType: "limit",
	}
}

// B1: first-seen oid emits wallet.order.created once (OrderDTO + status
// open); later updates for the same oid keep wallet.order.updated.
func TestWatcher_OrderCreatedOncePerOid(t *testing.T) {
	mgr := NewWatcherManager(NewActivityHub(), nil, nil, 4)
	wallet := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	c := mgr.Subscribe(wallet)
	_ = drainRawEvents(c) // bootstrap: connection/position/state frames

	mgr.OnOrder(wallet, testOrder(111))
	evs := drainRawEvents(c)
	if len(evs) != 1 || evs[0].Type != EvtOrderCreated {
		t.Fatalf("first-seen oid must emit exactly 1 %s, got %+v", EvtOrderCreated, evs)
	}
	var created OrderCreatedPayload
	if err := json.Unmarshal(evs[0].Data, &created); err != nil {
		t.Fatalf("created payload decode: %v", err)
	}
	if created.Status != "open" || created.Oid != 111 || created.Coin != "BTC" ||
		created.Side != "BUY" || created.LimitPx != 50000 || created.OrigSize != 1 {
		t.Errorf("created payload = OrderDTO + status open, got %+v", created)
	}

	// Second + third sightings of the same oid stay updated, never created.
	mgr.OnOrder(wallet, testOrder(111))
	mgr.OnOrder(wallet, testOrder(111))
	evs = drainRawEvents(c)
	if len(evs) != 2 {
		t.Fatalf("two updates must emit 2 frames, got %+v", evs)
	}
	for _, e := range evs {
		if e.Type != EvtOrderUpdated {
			t.Fatalf("repeat oid must emit %s, got %+v", EvtOrderUpdated, evs)
		}
		var upd OrderDTO
		if err := json.Unmarshal(e.Data, &upd); err != nil {
			t.Fatalf("updated payload decode: %v", err)
		}
		if upd.Oid != 111 {
			t.Errorf("updated oid: %+v", upd)
		}
		var raw map[string]any
		_ = json.Unmarshal(e.Data, &raw)
		if _, hasStatus := raw["status"]; hasStatus {
			t.Errorf("updated payload must keep the old shape (no status field), got %v", raw)
		}
	}

	// A different oid gets its own single created.
	mgr.OnOrder(wallet, testOrder(222))
	evs = drainRawEvents(c)
	if len(evs) != 1 || evs[0].Type != EvtOrderCreated {
		t.Fatalf("new oid must emit %s once, got %+v", EvtOrderCreated, evs)
	}

	snap := mgr.Snapshot(wallet)
	if len(snap.Orders) != 2 {
		t.Errorf("snapshot must hold both oids, got %d", len(snap.Orders))
	}

	// Unwatched wallet is a no-op (no panic, no event).
	mgr.OnOrder("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", testOrder(333))
}

// B2 mapping: mapUserFills preserves dir verbatim + parses closedPnl, and
// the DTO JSON envelope always carries dir + closed_pnl keys.
func TestMapUserFills_PreservesDirClosedPnl(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond).UnixMilli()
	raw := []hyperliquid.Fill{
		{Coin: "BTC", Side: "B", Sz: "1", Px: "50000", Time: now,
			Dir: "Open Long", ClosedPnl: "0", Fee: "1", FeeToken: "USDC",
			Tid: 1, Oid: 10, Crossed: true, StartPosition: "0"},
		{Coin: "ETH", Side: "A", Sz: "2", Px: "3000", Time: now,
			Dir: "Close Long", ClosedPnl: "12.5", Fee: "0.5", FeeToken: "USDC",
			Tid: 2, Oid: 11, StartPosition: "2"},
		{Coin: "BTC", Side: "B", Sz: "0", Px: "1", Time: now,
			Dir: "Open Long", ClosedPnl: "0", Tid: 3}, // skipped: sz <= 0
	}
	mapped := mapUserFills(raw)
	if len(mapped) != 2 {
		t.Fatalf("want 2 mapped (1 bad skipped), got %d", len(mapped))
	}
	byTid := map[int64]FillDTO{}
	for _, f := range mapped {
		byTid[f.Tid] = f
	}
	if byTid[1].Dir != "Open Long" || byTid[1].ClosedPnl != 0 {
		t.Errorf("fill 1 dir/closedPnl: %+v", byTid[1])
	}
	if byTid[2].Dir != "Close Long" || byTid[2].ClosedPnl != 12.5 {
		t.Errorf("fill 2 dir/closedPnl: %+v", byTid[2])
	}
	for _, f := range mapped {
		rawJSON, _ := json.Marshal(f)
		var keys map[string]any
		if err := json.Unmarshal(rawJSON, &keys); err != nil {
			t.Fatalf("dto json: %v", err)
		}
		if _, ok := keys["dir"]; !ok {
			t.Errorf("FillDTO JSON must always carry dir, got %s", rawJSON)
		}
		if _, ok := keys["closed_pnl"]; !ok {
			t.Errorf("FillDTO JSON must always carry closed_pnl, got %s", rawJSON)
		}
	}
}

// B2 envelope: OnFill emits wallet.fill.created whose data carries dir +
// closedPnl (watcher path, not the legacy {type:activity} harvest path).
func TestWatcher_OnFillEmitsDirClosedPnl(t *testing.T) {
	mgr := NewWatcherManager(NewActivityHub(), nil, nil, 4)
	wallet := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	c := mgr.Subscribe(wallet)
	_ = drainRawEvents(c)

	fill := FillDTO{
		Coin: "BTC", Side: "BUY", Dir: "Open Long", Size: 1, Price: 50000,
		ClosedPnl: 0, Fee: 1, FeeToken: "USDC",
		Time: time.Now().UTC().Truncate(time.Millisecond), Tid: 99, Oid: 7,
		Crossed: true, StartPosition: "0",
	}
	mgr.OnFill(wallet, fill)
	evs := drainRawEvents(c)
	if len(evs) == 0 || evs[0].Type != EvtFillCreated {
		t.Fatalf("first frame must be %s, got %+v", EvtFillCreated, evs)
	}
	var got FillDTO
	if err := json.Unmarshal(evs[0].Data, &got); err != nil {
		t.Fatalf("fill data decode: %v", err)
	}
	if got.Dir != "Open Long" || got.ClosedPnl != 0 {
		t.Errorf("fill envelope must carry dir + closedPnl, got %+v", got)
	}
	var raw map[string]any
	_ = json.Unmarshal(evs[0].Data, &raw)
	if raw["dir"] != "Open Long" {
		t.Errorf("fill JSON dir: %v", raw)
	}
	if raw["closed_pnl"] != float64(0) {
		t.Errorf("fill JSON closed_pnl: %v", raw)
	}
}

// B2 watcher path: OnRawUserFill maps one hyperliquid.Fill via the same
// rules (invalid rows skipped) so streaming userFills rows keep dir.
func TestWatcher_OnRawUserFill_MapsDir(t *testing.T) {
	mgr := NewWatcherManager(NewActivityHub(), nil, nil, 4)
	wallet := "0xcccccccccccccccccccccccccccccccccccccccc"
	c := mgr.Subscribe(wallet)
	_ = drainRawEvents(c)

	now := time.Now().UTC().Truncate(time.Millisecond).UnixMilli()
	ok := mgr.OnRawUserFill(wallet, hyperliquid.Fill{
		Coin: "BTC", Side: "B", Sz: "1", Px: "50000", Time: now,
		Dir: "Open Short", ClosedPnl: "-3.25", Fee: "0.1", FeeToken: "USDC",
		Tid: 55, Oid: 9, StartPosition: "0",
	})
	if !ok {
		t.Fatal("valid userFills row must be emitted")
	}
	evs := drainRawEvents(c)
	if len(evs) == 0 || evs[0].Type != EvtFillCreated {
		t.Fatalf("want %s, got %+v", EvtFillCreated, evs)
	}
	var got FillDTO
	_ = json.Unmarshal(evs[0].Data, &got)
	if got.Dir != "Open Short" || got.ClosedPnl != -3.25 {
		t.Errorf("raw fill mapping must keep dir/closedPnl, got %+v", got)
	}

	if mgr.OnRawUserFill(wallet, hyperliquid.Fill{
		Coin: "BTC", Side: "B", Sz: "0", Px: "1", Time: now, Dir: "Open Long", Tid: 56,
	}) {
		t.Error("invalid row (sz<=0) must be skipped")
	}
	if len(drainRawEvents(c)) != 0 {
		t.Error("skipped row must emit nothing")
	}
	if mgr.OnRawUserFill("0xdddddddddddddddddddddddddddddddddddddddd", hyperliquid.Fill{
		Coin: "BTC", Side: "B", Sz: "1", Px: "1", Time: now, Dir: "Open Long", Tid: 57,
	}) {
		t.Error("unwatched wallet must not emit")
	}
}
