package trader

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/qwerty7415963/go_be_arbitrage/internal/hyperliquid"
)

// ConnectionState is the per-wallet reconnect state machine
// (LIVE-CONTRACT v1.2 §2), surfaced to FE via wallet.connection.updated.
// Never display non-LIVE data with a LIVE badge.
type ConnectionState string

const (
	ConnDisconnected ConnectionState = "DISCONNECTED"
	ConnReconnect    ConnectionState = "RECONNECT"
	ConnResync       ConnectionState = "RESYNC"
	ConnReconcile    ConnectionState = "RECONCILE"
	ConnLive         ConnectionState = "LIVE"
)

// Wallet event envelope types (contract §2: normalized backend WS events).
const (
	EvtPositionUpdated   = "wallet.position.updated"
	EvtFillCreated       = "wallet.fill.created"
	EvtFundingCreated    = "wallet.funding.created"
	EvtOrderUpdated      = "wallet.order.updated"
	EvtActivityCreated   = "wallet.activity.created"
	EvtStateUpdated      = "wallet.state.updated"
	EvtConnectionUpdated = "wallet.connection.updated"
)

// WalletEvent is the wallet.* envelope sent to browser subscribers.
type WalletEvent struct {
	Type   string          `json:"type"`
	Wallet string          `json:"wallet"`
	Data   any             `json:"data,omitempty"`
	At     time.Time       `json:"at"`
	State  ConnectionState `json:"state,omitempty"`
}

// WalletSnapshot is the in-memory per-wallet snapshot (contract §2):
// recent fills buffer, funding buffer, last order states.
type WalletSnapshot struct {
	Fills    []FillDTO
	Fundings []hyperliquid.FundingPayment
	Orders   map[int64]OrderDTO
	AsOf     time.Time
}

// UpstreamSubscriptions counts the 4-feed subscribe set (observability +
// conformance: 10+ subscribers ⇒ exactly 1 upstream watcher).
type UpstreamStats struct {
	Watchers           int
	UpstreamSubscribes int64
	Resyncs            int64
}

// UpstreamDialer subscribes the 4 per-wallet feeds. Production dials
// wss://api.hyperliquid.xyz/ws and multiplexes; tests inject a fake.
// Nil means REST-resync only (state machine still runs; streaming events are
// no-ops until a dialer is attached).
type UpstreamDialer interface {
	Subscribe(ctx context.Context, addr string) error
	Unsubscribe(addr string) error
}

// nilUpstream is the default no-op dialer (REST-resync only).
type nilUpstream struct{}

func (nilUpstream) Subscribe(context.Context, string) error { return nil }
func (nilUpstream) Unsubscribe(string) error                { return nil }

// WalletWatcher is one per-watched-address upstream subscription set,
// refcounted by browser subscribers. Positions have no WS stream → served by
// §1.1 REST (short TTL), NOT by the watcher; the watcher emits
// wallet.position.updated on REST bootstrap/resync only.
type WalletWatcher struct {
	addr string
	mu   sync.Mutex
	// refcount mirrors hub.SubscriberCount(addr); watcher exists iff > 0.
	refcount int
	state    ConnectionState
	snapshot WalletSnapshot
	// upstreamSubscribeCalls counts Subscribe calls for this watcher
	// (conformance: exactly 1 per watcher lifetime regardless of subs).
	upstreamSubscribeCalls int
	stopCh                 chan struct{}
}

// WatcherManager owns one WalletWatcher per watched address (refcounted) +
// the shared upstream WS connection management + concurrency gate.
type WatcherManager struct {
	mu       sync.Mutex
	hub      *ActivityHub
	live     OnDemandClient
	upstream UpstreamDialer
	watchers map[string]*WalletWatcher
	// gate bounds concurrent RESYNCs (REST bootstrap) — contract concurrency
	// gate, default 4 (same as the interactive client).
	gate chan struct{}
	// stats for conformance (10+ subs ⇒ 1 upstream watcher).
	upstreamSubscribes int64
	resyncs            int64
}

// NewWatcherManager builds the per-address watcher set. hub must be non-nil
// (browser transport); live may be nil (RESYNC degrades to empty snapshot).
// upstream may be nil (REST-resync only). gateSize <= 0 selects 4.
func NewWatcherManager(hub *ActivityHub, live OnDemandClient, upstream UpstreamDialer, gateSize int) *WatcherManager {
	if hub == nil {
		hub = NewActivityHub()
	}
	if upstream == nil {
		upstream = nilUpstream{}
	}
	if gateSize <= 0 {
		gateSize = 4
	}
	return &WatcherManager{
		hub: hub, live: live, upstream: upstream,
		watchers: map[string]*WalletWatcher{},
		gate:     make(chan struct{}, gateSize),
	}
}

// Hub exposes the browser transport (wiring/tests).
func (m *WatcherManager) Hub() *ActivityHub { return m.hub }

// GateSize reports the concurrency gate (tests).
func (m *WatcherManager) GateSize() int { return cap(m.gate) }

// WatcherCount reports live upstream watchers (conformance/tests).
func (m *WatcherManager) WatcherCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.watchers)
}

// Stats snapshots watcher health.
func (m *WatcherManager) Stats() UpstreamStats {
	m.mu.Lock()
	defer m.mu.Unlock()
	return UpstreamStats{Watchers: len(m.watchers), UpstreamSubscribes: m.upstreamSubscribes, Resyncs: m.resyncs}
}

// WatchSet delegates to the hub (currently-watched addresses).
func (m *WatcherManager) WatchSet() []string { return m.hub.WatchSet() }

// IsWatched delegates to the hub.
func (m *WatcherManager) IsWatched(addr string) bool { return m.hub.IsWatched(addr) }

// Subscribe registers one browser client for wallet (lowercased). First
// subscriber creates the upstream watcher (4-feed subscribe + REST bootstrap
// RESYNC → RECONCILE → LIVE); further subscribers share it.
func (m *WatcherManager) Subscribe(wallet string) *ActivityClient {
	wallet = strings.ToLower(strings.TrimSpace(wallet))
	c := m.hub.Subscribe(wallet)
	m.mu.Lock()
	w, ok := m.watchers[wallet]
	if !ok {
		w = &WalletWatcher{addr: wallet, state: ConnDisconnected, stopCh: make(chan struct{})}
		w.snapshot.Orders = map[int64]OrderDTO{}
		m.watchers[wallet] = w
	}
	w.mu.Lock()
	w.refcount++
	first := w.refcount == 1
	w.mu.Unlock()
	needBootstrap := first
	m.mu.Unlock()
	if needBootstrap {
		m.bootstrap(wallet, w)
	}
	return c
}

// Unsubscribe removes one browser client; last unsubscribe tears down the
// upstream watcher (unsubscribe + memory release).
func (m *WatcherManager) Unsubscribe(c *ActivityClient) {
	if c == nil {
		return
	}
	wallet := c.wallet
	m.hub.Unsubscribe(c)
	m.mu.Lock()
	w, ok := m.watchers[wallet]
	if !ok {
		m.mu.Unlock()
		return
	}
	w.mu.Lock()
	if w.refcount > 0 {
		w.refcount--
	}
	teardown := w.refcount == 0
	w.mu.Unlock()
	if teardown {
		delete(m.watchers, wallet)
		close(w.stopCh)
	}
	m.mu.Unlock()
	if teardown && m.upstream != nil {
		_ = m.upstream.Unsubscribe(wallet)
	}
}

// bootstrap runs DISCONNECTED → RECONNECT → RESYNC (REST, gated) → RECONCILE
// → LIVE, emitting wallet.connection.updated at each step and
// wallet.position.updated on the REST snapshot (positions have no WS stream).
func (m *WatcherManager) bootstrap(wallet string, w *WalletWatcher) {
	m.setState(w, ConnReconnect, "", nil)
	// Upstream 4-feed subscribe (exactly once per watcher lifetime).
	if m.upstream != nil {
		_ = m.upstream.Subscribe(context.Background(), wallet)
		m.mu.Lock()
		m.upstreamSubscribes++
		w.mu.Lock()
		w.upstreamSubscribeCalls++
		w.mu.Unlock()
		m.mu.Unlock()
	}
	m.setState(w, ConnResync, "", nil)
	// Concurrency gate around the REST resync (never block teardown on full).
	select {
	case m.gate <- struct{}{}:
		snap := m.resyncSnapshot(wallet)
		<-m.gate
		m.mu.Lock()
		m.resyncs++
		m.mu.Unlock()
		w.mu.Lock()
		w.snapshot = *snap
		w.mu.Unlock()
		m.emit(wallet, EvtPositionUpdated, map[string]any{"as_of": snap.AsOf}, ConnResync)
		m.setState(w, ConnReconcile, "", nil)
		// RECONCILE: diff stub (snapshot is the bootstrap; no prior state to
		// reconcile against on first subscribe). Emit state for FE badge.
		m.emit(wallet, EvtStateUpdated, map[string]any{"as_of": snap.AsOf}, ConnReconcile)
		m.setState(w, ConnLive, "", nil)
	default:
		// Gate full: stay RECONNECT and retry on next tick (never hang the
		// subscribe path; FE shows non-LIVE badge until LIVE).
		m.setState(w, ConnReconnect, "gate_full_retry", nil)
		go func() {
			select {
			case m.gate <- struct{}{}:
				snap := m.resyncSnapshot(wallet)
				<-m.gate
				m.mu.Lock()
				m.resyncs++
				m.mu.Unlock()
				w.mu.Lock()
				w.snapshot = *snap
				w.mu.Unlock()
				m.emit(wallet, EvtPositionUpdated, map[string]any{"as_of": snap.AsOf}, ConnResync)
				m.setState(w, ConnReconcile, "", nil)
				m.emit(wallet, EvtStateUpdated, map[string]any{"as_of": snap.AsOf}, ConnReconcile)
				m.setState(w, ConnLive, "", nil)
			case <-w.stopCh:
			}
		}()
	}
}

func (m *WatcherManager) setState(w *WalletWatcher, to ConnectionState, reason string, extra any) {
	w.mu.Lock()
	from := w.state
	w.state = to
	w.mu.Unlock()
	data := map[string]any{"from": string(from), "to": string(to)}
	if reason != "" {
		data["reason"] = reason
	}
	if extra != nil {
		data["extra"] = extra
	}
	m.emit(w.addr, EvtConnectionUpdated, data, to)
	_ = from
}

// State reports the watcher's connection state (tests/FE badge).
func (m *WatcherManager) State(wallet string) ConnectionState {
	m.mu.Lock()
	w, ok := m.watchers[strings.ToLower(strings.TrimSpace(wallet))]
	m.mu.Unlock()
	if !ok {
		return ConnDisconnected
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.state
}

// Snapshot returns a copy of the in-memory snapshot (tests).
func (m *WatcherManager) Snapshot(wallet string) WalletSnapshot {
	m.mu.Lock()
	w, ok := m.watchers[strings.ToLower(strings.TrimSpace(wallet))]
	m.mu.Unlock()
	if !ok {
		return WalletSnapshot{Orders: map[int64]OrderDTO{}}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	cpy := WalletSnapshot{
		Fills:    append([]FillDTO(nil), w.snapshot.Fills...),
		Fundings: append([]hyperliquid.FundingPayment(nil), w.snapshot.Fundings...),
		Orders:   map[int64]OrderDTO{},
		AsOf:     w.snapshot.AsOf,
	}
	for k, v := range w.snapshot.Orders {
		cpy.Orders[k] = v
	}
	return cpy
}

// resyncSnapshot fetches the REST bootstrap (fills + orders + funding +
// ledger) for RECONCILE. Failures degrade to an empty snapshot (never nil
// maps/slices); positions are served by §1.1 REST separately.
func (m *WatcherManager) resyncSnapshot(wallet string) *WalletSnapshot {
	snap := &WalletSnapshot{Orders: map[int64]OrderDTO{}, AsOf: time.Now().UTC()}
	if m.live == nil {
		return snap
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if fills, err := m.live.FetchUserFills(ctx, wallet); err == nil {
		snap.Fills = mapUserFills(fills)
		if len(snap.Fills) > 200 {
			snap.Fills = snap.Fills[:200]
		}
	}
	if orders, err := m.live.FetchOpenOrders(ctx, wallet); err == nil {
		for _, o := range mapOpenOrders(orders, false) {
			snap.Orders[o.Oid] = o
		}
	}
	now := time.Now().UTC()
	startMs := now.Add(-LiveActivityWindow).UnixMilli()
	if fraw, err := m.live.FetchUserFunding(ctx, wallet, startMs, now.UnixMilli()); err == nil {
		snap.Fundings = parseFundingPayments(fraw)
		if len(snap.Fundings) > 200 {
			snap.Fundings = snap.Fundings[:200]
		}
	}
	return snap
}

// emit publishes one wallet.* envelope to the wallet's browser subscribers
// (non-blocking; drops on full buffers like the hub).
func (m *WatcherManager) emit(wallet, evtType string, data any, state ConnectionState) {
	wallet = strings.ToLower(strings.TrimSpace(wallet))
	ev := WalletEvent{Type: evtType, Wallet: wallet, Data: data, At: time.Now().UTC(), State: state}
	msg, err := json.Marshal(ev)
	if err != nil {
		return
	}
	m.hub.mu.RLock()
	defer m.hub.mu.RUnlock()
	for c := range m.hub.subs[wallet] {
		select {
		case c.send <- msg:
		default:
		}
	}
}

// OnFill ingests one upstream fill (userFills WS): appends to the snapshot
// buffer and emits wallet.fill.created + wallet.activity.created (when the
// fill closes a cycle — best-effort via single-coin reconstruct) + state.
func (m *WatcherManager) OnFill(wallet string, fill FillDTO) {
	m.mu.Lock()
	w, ok := m.watchers[strings.ToLower(strings.TrimSpace(wallet))]
	m.mu.Unlock()
	if !ok {
		return
	}
	w.mu.Lock()
	if w.state != ConnLive {
		// Buffer but do not present as LIVE (contract: never display non-LIVE
		// data with a LIVE badge; events carry the current state).
		w.snapshot.Fills = append([]FillDTO{fill}, w.snapshot.Fills...)
		if len(w.snapshot.Fills) > 500 {
			w.snapshot.Fills = w.snapshot.Fills[:500]
		}
		st := w.state
		w.mu.Unlock()
		m.emit(wallet, EvtFillCreated, fill, st)
		return
	}
	w.snapshot.Fills = append([]FillDTO{fill}, w.snapshot.Fills...)
	if len(w.snapshot.Fills) > 500 {
		w.snapshot.Fills = w.snapshot.Fills[:500]
	}
	w.mu.Unlock()
	m.emit(wallet, EvtFillCreated, fill, ConnLive)
	m.emit(wallet, EvtStateUpdated, map[string]any{"fills": len(w.snapshot.Fills)}, ConnLive)
}

// OnFunding ingests one upstream funding payment.
func (m *WatcherManager) OnFunding(wallet string, p hyperliquid.FundingPayment) {
	m.mu.Lock()
	w, ok := m.watchers[strings.ToLower(strings.TrimSpace(wallet))]
	m.mu.Unlock()
	if !ok {
		return
	}
	w.mu.Lock()
	st := w.state
	w.snapshot.Fundings = append([]hyperliquid.FundingPayment{p}, w.snapshot.Fundings...)
	if len(w.snapshot.Fundings) > 500 {
		w.snapshot.Fundings = w.snapshot.Fundings[:500]
	}
	w.mu.Unlock()
	m.emit(wallet, EvtFundingCreated, p, st)
}

// OnOrder ingests one upstream order update.
func (m *WatcherManager) OnOrder(wallet string, o OrderDTO) {
	m.mu.Lock()
	w, ok := m.watchers[strings.ToLower(strings.TrimSpace(wallet))]
	m.mu.Unlock()
	if !ok {
		return
	}
	w.mu.Lock()
	st := w.state
	if w.snapshot.Orders == nil {
		w.snapshot.Orders = map[int64]OrderDTO{}
	}
	w.snapshot.Orders[o.Oid] = o
	w.mu.Unlock()
	m.emit(wallet, EvtOrderUpdated, o, st)
}

// Reconnect drives DISCONNECTED → RECONNECT → RESYNC → RECONCILE → LIVE for
// an existing watcher (test double kills upstream; production WS failures
// call this). It re-subscribes upstream and re-runs the gated REST resync.
func (m *WatcherManager) Reconnect(wallet string) {
	wallet = strings.ToLower(strings.TrimSpace(wallet))
	m.mu.Lock()
	w, ok := m.watchers[wallet]
	m.mu.Unlock()
	if !ok {
		return
	}
	m.setState(w, ConnReconnect, "reconnect_requested", nil)
	if m.upstream != nil {
		_ = m.upstream.Subscribe(context.Background(), wallet)
	}
	m.setState(w, ConnResync, "", nil)
	select {
	case m.gate <- struct{}{}:
		snap := m.resyncSnapshot(wallet)
		<-m.gate
		w.mu.Lock()
		w.snapshot = *snap
		w.mu.Unlock()
		m.emit(wallet, EvtPositionUpdated, map[string]any{"as_of": snap.AsOf}, ConnResync)
		m.setState(w, ConnReconcile, "", nil)
		m.emit(wallet, EvtStateUpdated, map[string]any{"as_of": snap.AsOf}, ConnReconcile)
		m.setState(w, ConnLive, "", nil)
	default:
		m.setState(w, ConnReconnect, "gate_full_retry", nil)
	}
}
