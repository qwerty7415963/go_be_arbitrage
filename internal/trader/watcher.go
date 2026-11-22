package trader

import (
	"context"
	"encoding/json"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

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
	EvtOrderCreated      = "wallet.order.created"
	EvtActivityCreated   = "wallet.activity.created"
	EvtStateUpdated      = "wallet.state.updated"
	EvtConnectionUpdated = "wallet.connection.updated"
)

// OrderCreatedPayload is the wallet.order.created data (SIDEBAR-3EVENTS D2):
// OrderDTO fields flattened + status:"open" for sidebar limit rows
// (coin, oid, status open, side, limit_px, size/orig_size, timestamp).
type OrderCreatedPayload struct {
	OrderDTO
	Status string `json:"status"`
}

// toOrderCreatedPayload wraps an open order with status open.
func toOrderCreatedPayload(o OrderDTO) OrderCreatedPayload {
	return OrderCreatedPayload{OrderDTO: o, Status: "open"}
}

// WalletEvent is the wallet.* envelope sent to browser subscribers.
type WalletEvent struct {
	Type   string          `json:"type"`
	Wallet string          `json:"wallet"`
	UserID uuid.UUID       `json:"user_id,omitempty"`
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
	Watchers               int
	UpstreamSubscribes     int64
	Resyncs                int64
	ObserverDropped        int64
	PositionRefreshes      int64
	PositionRefreshDropped int64
	PositionRefreshErrors  int64
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

// PositionSnapshotProvider builds a full position snapshot DTO for a wallet.
type PositionSnapshotProvider func(ctx context.Context, wallet string) (*PositionSnapshotDTO, error)

// activityProvider is the seam for server-side bootstrap (trades); *Service
// satisfies it. Tests may inject a fake.
type activityProvider interface {
	Activity(ctx context.Context, venueCode, rawAddr string, q ActivityQuery) (*ActivityPage, error)
}

// WalletWatcher is one per-watched-address upstream subscription set,
// refcounted by browser subscribers. Positions have no WS stream → served by
// §1.1 REST (short TTL), NOT by the watcher; the watcher emits
// wallet.position.updated on REST bootstrap/resync only.
type WalletWatcher struct {
	addr string
	mu   sync.Mutex
	// browserRefs mirrors hub subscriber count; retained is non-browser refcount.
	browserRefs  int
	retained     int
	bootstrapped bool
	state        ConnectionState
	snapshot     WalletSnapshot
	// upstreamSubscribeCalls counts Subscribe calls for this watcher
	// (conformance: exactly 1 per watcher lifetime regardless of subs).
	upstreamSubscribeCalls int
	stopCh                 chan struct{}

	// Position refresh machinery (contract v2.3).
	positionTimer      *time.Timer
	positionPending    bool
	positionRefreshing bool
	lastPositionDTO    *PositionSnapshotDTO

	// Channel subscription (contract v3.0).
	channels   map[string]bool
	droppedSeq int64
}

// WatcherManager owns one WalletWatcher per watched address (refcounted) +
// the shared upstream WS connection management + concurrency gate.
type WatcherManager struct {
	mu       sync.Mutex
	hub      *ActivityHub
	live     OnDemandClient
	upstream UpstreamDialer
	watchers map[string]*WalletWatcher
	// gate bounds concurrent RESYNCs (REST bootstrap) – contract concurrency
	// gate, default 4 (same as the interactive client).
	gate chan struct{}
	// stats for conformance (10+ subs → 1 upstream watcher).
	upstreamSubscribes int64
	resyncs            int64

	// Position snapshot provider (contract v2.3).
	positionProvider PositionSnapshotProvider

	// Service for server-side bootstrap (trades) (contract v3.0).
	service activityProvider

	// position refresh counters.
	positionRefreshes      int64
	positionRefreshDropped int64
	positionRefreshErrors  int64

	// observer fan-out (buffered, non-blocking).
	observerMu   sync.Mutex
	observers    []func(WalletEvent)
	observerCh   chan WalletEvent
	observerDrop int64
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
	m := &WatcherManager{
		hub: hub, live: live, upstream: upstream,
		watchers:   map[string]*WalletWatcher{},
		gate:       make(chan struct{}, gateSize),
		observerCh: make(chan WalletEvent, 1024),
	}
	go m.runObserver()
	return m
}

// WithPositionProvider attaches the position snapshot provider.
func (m *WatcherManager) WithPositionProvider(p PositionSnapshotProvider) *WatcherManager {
	if m == nil {
		return m
	}
	m.mu.Lock()
	m.positionProvider = p
	m.mu.Unlock()
	return m
}

// WithService attaches the trader service for server-side bootstrap (trades).
func (m *WatcherManager) WithService(s activityProvider) *WatcherManager {
	if m == nil {
		return m
	}
	m.mu.Lock()
	m.service = s
	m.mu.Unlock()
	return m
}

// AddObserver registers an observer invoked from emit after fan-out to
// browser clients. The call never blocks emit; events are enqueued into a
// buffered channel (capacity 1024) and dropped on overflow (counted).
func (m *WatcherManager) AddObserver(fn func(WalletEvent)) {
	if fn == nil || m == nil {
		return
	}
	m.observerMu.Lock()
	m.observers = append(m.observers, fn)
	m.observerMu.Unlock()
}

func (m *WatcherManager) runObserver() {
	for ev := range m.observerCh {
		m.observerMu.Lock()
		obs := append([]func(WalletEvent){}, m.observers...)
		m.observerMu.Unlock()
		for _, fn := range obs {
			fn(ev)
		}
	}
}

// Retain keeps a watcher alive for a non-browser subscriber (e.g. notifier).
func (m *WatcherManager) Retain(addr string) error {
	if m == nil {
		return nil
	}
	addr = strings.ToLower(strings.TrimSpace(addr))
	m.mu.Lock()
	w, ok := m.watchers[addr]
	if !ok {
		w = &WalletWatcher{addr: addr, state: ConnDisconnected, stopCh: make(chan struct{})}
		w.snapshot.Orders = map[int64]OrderDTO{}
		m.watchers[addr] = w
	}
	w.mu.Lock()
	w.retained++
	needBootstrap := !w.bootstrapped
	if needBootstrap {
		w.bootstrapped = true
	}
	w.mu.Unlock()
	m.mu.Unlock()
	if needBootstrap {
		m.bootstrap(addr, w)
	}
	return nil
}

// Release decrements the non-browser refcount; tears down when both
// browser and retained counts are zero.
func (m *WatcherManager) Release(addr string) {
	if m == nil {
		return
	}
	addr = strings.ToLower(strings.TrimSpace(addr))
	m.mu.Lock()
	w, ok := m.watchers[addr]
	if !ok {
		m.mu.Unlock()
		return
	}
	w.mu.Lock()
	if w.retained > 0 {
		w.retained--
	}
	teardown := w.browserRefs == 0 && w.retained == 0
	if teardown && w.positionTimer != nil {
		w.positionTimer.Stop()
		w.positionTimer = nil
	}
	w.mu.Unlock()
	if teardown {
		delete(m.watchers, addr)
		close(w.stopCh)
	}
	m.mu.Unlock()
	if teardown && m.upstream != nil {
		_ = m.upstream.Unsubscribe(addr)
	}
}

// ObserverDropped reports the number of observer events dropped on overflow.
func (m *WatcherManager) ObserverDropped() int64 {
	if m == nil {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.observerDrop
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
	return UpstreamStats{Watchers: len(m.watchers), UpstreamSubscribes: m.upstreamSubscribes, Resyncs: m.resyncs, ObserverDropped: m.observerDrop, PositionRefreshes: m.positionRefreshes, PositionRefreshDropped: m.positionRefreshDropped, PositionRefreshErrors: m.positionRefreshErrors}
}

// WatchSet delegates to the hub (currently-watched addresses).
func (m *WatcherManager) WatchSet() []string { return m.hub.WatchSet() }

// IsWatched delegates to the hub.
func (m *WatcherManager) IsWatched(addr string) bool { return m.hub.IsWatched(addr) }

func defaultWatcherChannels() map[string]bool {
	return map[string]bool{"positions": true, "orders": true, "activity": true, "trades": true}
}

func normalizeChannels(chs []string) map[string]bool {
	if len(chs) == 0 {
		return defaultWatcherChannels()
	}
	out := map[string]bool{"positions": false, "orders": false, "activity": false, "trades": false}
	for _, ch := range chs {
		ch = strings.ToLower(strings.TrimSpace(ch))
		if _, ok := out[ch]; ok {
			out[ch] = true
		}
	}
	any := false
	for _, v := range out {
		if v {
			any = true
			break
		}
	}
	if !any {
		return defaultWatcherChannels()
	}
	return out
}

// Subscribe registers one browser client for wallet (lowercased). First
// subscriber creates the upstream watcher (4-feed subscribe + REST bootstrap
// RESYNC → RECONCILE → LIVE); further subscribers share it.
func (m *WatcherManager) Subscribe(wallet string) *ActivityClient {
	return m.SubscribeWithChannels(wallet, nil)
}

// SubscribeWithChannels registers a browser client with an initial channel set.
func (m *WatcherManager) SubscribeWithChannels(wallet string, channels []string) *ActivityClient {
	wallet = strings.ToLower(strings.TrimSpace(wallet))
	c := m.hub.Subscribe(wallet, 256)
	m.mu.Lock()
	w, ok := m.watchers[wallet]
	if !ok {
		w = &WalletWatcher{addr: wallet, state: ConnDisconnected, stopCh: make(chan struct{}), channels: normalizeChannels(channels)}
		w.snapshot.Orders = map[int64]OrderDTO{}
		m.watchers[wallet] = w
	} else {
		if channels != nil {
			w.mu.Lock()
			w.channels = normalizeChannels(channels)
			w.mu.Unlock()
		}
		if w.channels == nil {
			w.mu.Lock()
			w.channels = defaultWatcherChannels()
			w.mu.Unlock()
		}
	}
	w.mu.Lock()
	if w.channels == nil {
		w.channels = defaultWatcherChannels()
	}
	w.browserRefs++
	needBootstrap := w.browserRefs == 1 && w.retained == 0 && !w.bootstrapped
	if !w.bootstrapped {
		needBootstrap = true
		w.bootstrapped = true
	}
	w.mu.Unlock()
	m.mu.Unlock()
	if needBootstrap {
		m.bootstrap(wallet, w)
	}
	return c
}

// SetClientChannels updates channel subscription for a watcher by wallet.
func (m *WatcherManager) SetClientChannels(wallet string, channels []string) {
	wallet = strings.ToLower(strings.TrimSpace(wallet))
	m.mu.Lock()
	w, ok := m.watchers[wallet]
	m.mu.Unlock()
	if !ok {
		return
	}
	w.mu.Lock()
	w.channels = normalizeChannels(channels)
	w.mu.Unlock()
}

// BootstrapChannels emits wallet.bootstrap.<channel> for the given channels.
func (m *WatcherManager) BootstrapChannels(wallet string, channels []string) {
	wallet = strings.ToLower(strings.TrimSpace(wallet))
	m.mu.Lock()
	w, ok := m.watchers[wallet]
	m.mu.Unlock()
	if !ok {
		return
	}
	if w.channels == nil {
		w.mu.Lock()
		w.channels = normalizeChannels(channels)
		w.mu.Unlock()
	}
	// ensure requested channels enabled
	if channels != nil {
		m.SetClientChannels(wallet, channels)
		// re-fetch watcher
		m.mu.Lock()
		w, _ = m.watchers[wallet]
		m.mu.Unlock()
	}
	if w == nil {
		return
	}
	m.mu.Lock()
	snap := w.snapshot
	m.mu.Unlock()
	m.emitBootstrapChannels(wallet, w, &snap)
}

// ChannelsEnabled reports channel subscription for wallet.
func (m *WatcherManager) ChannelsEnabled(wallet string) map[string]bool {
	wallet = strings.ToLower(strings.TrimSpace(wallet))
	m.mu.Lock()
	w, ok := m.watchers[wallet]
	m.mu.Unlock()
	if !ok {
		return defaultWatcherChannels()
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.channels == nil {
		return defaultWatcherChannels()
	}
	out := make(map[string]bool, len(w.channels))
	for k, v := range w.channels {
		out[k] = v
	}
	return out
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
	if w.browserRefs > 0 {
		w.browserRefs--
	}
	teardown := w.browserRefs == 0 && w.retained == 0
	if teardown && w.positionTimer != nil {
		w.positionTimer.Stop()
		w.positionTimer = nil
	}
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
		m.emitPositionSnapshot(wallet, w, snap, ConnResync)
		m.emitBootstrapChannels(wallet, w, snap)
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
				m.emitPositionSnapshot(wallet, w, snap, ConnResync)
				m.emitBootstrapChannels(wallet, w, snap)
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
	ch := channelForEvt(evtType)
	// Filter by channel subscription.
	m.mu.Lock()
	w, _ := m.watchers[wallet]
	m.mu.Unlock()
	if w != nil && ch != "" && !w.channelEnabled(ch) {
		return
	}
	ev := WalletEvent{Type: evtType, Wallet: wallet, Data: data, At: time.Now().UTC(), State: state}
	msg, err := json.Marshal(ev)
	if err != nil {
		return
	}
	m.hub.mu.RLock()
	for c := range m.hub.subs[wallet] {
		select {
		case c.send <- msg:
		default:
			// backpressure: emit dropped marker
			m.mu.Lock()
			if w2, ok := m.watchers[wallet]; ok {
				w2.droppedSeq++
				seq := w2.droppedSeq
				m.mu.Unlock()
				dropped := map[string]any{"type": "wallet.dropped", "data": map[string]any{"channel": ch, "seq": seq}}
				if b, err := json.Marshal(dropped); err == nil {
					select {
					case c.send <- b:
					default:
					}
				}
			} else {
				m.mu.Unlock()
			}
		}
	}
	m.hub.mu.RUnlock()

	// Observer enqueue (non-blocking) after fan-out, without holding locks.
	if m.observerCh != nil {
		select {
		case m.observerCh <- ev:
		default:
			m.mu.Lock()
			m.observerDrop++
			m.mu.Unlock()
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
		m.schedulePositionRefresh(wallet)
		return
	}
	w.snapshot.Fills = append([]FillDTO{fill}, w.snapshot.Fills...)
	if len(w.snapshot.Fills) > 500 {
		w.snapshot.Fills = w.snapshot.Fills[:500]
	}
	w.mu.Unlock()
	m.emit(wallet, EvtFillCreated, fill, ConnLive)
	m.emit(wallet, EvtStateUpdated, map[string]any{"fills": len(w.snapshot.Fills)}, ConnLive)
	m.schedulePositionRefresh(wallet)
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

// OnRawUserFill ingests one upstream userFills WS row (hyperliquid.Fill):
// maps via the same mapUserFills rules (side/sz/px/time validation) so the
// wallet.fill.created envelope always carries dir + closedPnl
// (SIDEBAR-3EVENTS D1). Returns false when the row is skipped (invalid) or
// the wallet is unwatched, true when emitted via OnFill.
func (m *WatcherManager) OnRawUserFill(wallet string, f hyperliquid.Fill) bool {
	mapped := mapUserFills([]hyperliquid.Fill{f})
	if len(mapped) != 1 {
		return false
	}
	m.mu.Lock()
	_, ok := m.watchers[strings.ToLower(strings.TrimSpace(wallet))]
	m.mu.Unlock()
	if !ok {
		return false
	}
	m.OnFill(wallet, mapped[0])
	return true
}

// OnOrder ingests one upstream order update (SIDEBAR-3EVENTS D2): first-seen
// oid (absent from snapshot.Orders) emits wallet.order.created once with
// payload OrderDTO + status open; later updates for the same oid keep
// wallet.order.updated as before (never re-emit created).
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
	_, exists := w.snapshot.Orders[o.Oid]
	w.snapshot.Orders[o.Oid] = o
	w.mu.Unlock()
	if !exists {
		m.emit(wallet, EvtOrderCreated, toOrderCreatedPayload(o), st)
		m.schedulePositionRefresh(wallet)
		return
	}
	m.emit(wallet, EvtOrderUpdated, o, st)
	m.schedulePositionRefresh(wallet)
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
		m.emitPositionSnapshot(wallet, w, snap, ConnResync)
		m.emitBootstrapChannels(wallet, w, snap)
		m.setState(w, ConnReconcile, "", nil)
		m.emit(wallet, EvtStateUpdated, map[string]any{"as_of": snap.AsOf}, ConnReconcile)
		m.setState(w, ConnLive, "", nil)
	default:
		m.setState(w, ConnReconnect, "gate_full_retry", nil)
	}
}

// emitPositionSnapshot emits full position DTO via provider if available, else
// falls back to the legacy {as_of} payload.
func (m *WatcherManager) emitPositionSnapshot(wallet string, w *WalletWatcher, snap *WalletSnapshot, state ConnectionState) {
	if m.positionProvider != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		dto, err := m.positionProvider(ctx, wallet)
		cancel()
		if err == nil {
			if w != nil {
				w.mu.Lock()
				w.lastPositionDTO = dto
				w.mu.Unlock()
			}
			m.emit(wallet, EvtPositionUpdated, dto, state)
			return
		}
		m.mu.Lock()
		m.positionRefreshErrors++
		m.mu.Unlock()
		slog.Warn("position refresh failed", "wallet", wallet, "err", err)
	}
	m.emit(wallet, EvtPositionUpdated, map[string]any{"as_of": snap.AsOf}, state)
}

// schedulePositionRefresh debounces position refreshes per wallet (2s).
func (m *WatcherManager) schedulePositionRefresh(wallet string) {
	wallet = strings.ToLower(strings.TrimSpace(wallet))
	m.mu.Lock()
	w, ok := m.watchers[wallet]
	if !ok || m.positionProvider == nil {
		m.mu.Unlock()
		return
	}
	w.mu.Lock()
	if w.positionTimer != nil {
		w.positionTimer.Reset(2 * time.Second)
	} else {
		w.positionTimer = time.AfterFunc(2*time.Second, func() {
			m.runPositionRefresh(wallet)
		})
	}
	w.mu.Unlock()
	m.mu.Unlock()
}

// runPositionRefresh performs one debounced position refresh.
func (m *WatcherManager) runPositionRefresh(wallet string) {
	wallet = strings.ToLower(strings.TrimSpace(wallet))
	m.mu.Lock()
	w, ok := m.watchers[wallet]
	if !ok {
		m.mu.Unlock()
		return
	}
	w.mu.Lock()
	if w.positionRefreshing {
		w.positionPending = true
		w.mu.Unlock()
		m.mu.Unlock()
		return
	}
	w.positionRefreshing = true
	w.mu.Unlock()
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		w2, ok := m.watchers[wallet]
		var pending bool
		if ok {
			w2.mu.Lock()
			w2.positionRefreshing = false
			pending = w2.positionPending
			w2.positionPending = false
			w2.mu.Unlock()
		}
		m.mu.Unlock()
		if pending {
			m.schedulePositionRefresh(wallet)
		}
	}()

	// Non-blocking gate acquire.
	select {
	case m.gate <- struct{}{}:
	default:
		m.mu.Lock()
		w.mu.Lock()
		w.positionPending = true
		m.positionRefreshDropped++
		w.mu.Unlock()
		m.mu.Unlock()
		return
	}
	defer func() { <-m.gate }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dto, err := m.positionProvider(ctx, wallet)
	if err != nil {
		m.mu.Lock()
		m.positionRefreshErrors++
		m.mu.Unlock()
		slog.Warn("position refresh failed", "wallet", wallet, "err", err)
		return
	}
	m.mu.Lock()
	w.mu.Lock()
	w.lastPositionDTO = dto
	w.mu.Unlock()
	m.mu.Unlock()
	m.emit(wallet, EvtPositionUpdated, dto, ConnLive)
	m.mu.Lock()
	m.positionRefreshes++
	m.mu.Unlock()
}

// channelEnabled reports whether a channel is subscribed for this watcher.
func (w *WalletWatcher) channelEnabled(ch string) bool {
	if w == nil {
		return true
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.channels == nil {
		return true
	}
	v, ok := w.channels[ch]
	if !ok {
		return true
	}
	return v
}

func (m *WatcherManager) emitBootstrapChannels(wallet string, w *WalletWatcher, snap *WalletSnapshot) {
	if w == nil {
		return
	}
	// positions
	if w.channelEnabled("positions") {
		var pos *PositionSnapshotDTO
		w.mu.Lock()
		if w.lastPositionDTO != nil {
			pos = w.lastPositionDTO
		}
		w.mu.Unlock()
		if pos == nil && m.positionProvider != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			if p, err := m.positionProvider(ctx, wallet); err == nil {
				pos = p
				w.mu.Lock()
				w.lastPositionDTO = p
				w.mu.Unlock()
			}
			cancel()
		}
		if pos == nil {
			pos = &PositionSnapshotDTO{Positions: []PositionDTO{}, DataStatus: DataReady}
		}
		m.emit(wallet, "wallet.bootstrap.positions", pos, w.state)
	}
	// orders
	if w.channelEnabled("orders") {
		orders := make([]OrderDTO, 0)
		if snap != nil {
			for _, o := range snap.Orders {
				orders = append(orders, o)
			}
		}
		sort.Slice(orders, func(i, j int) bool { return orders[i].Oid < orders[j].Oid })
		m.emit(wallet, "wallet.bootstrap.orders", orders, w.state)
	}
	// activity
	if w.channelEnabled("activity") {
		fills := []FillDTO{}
		if snap != nil {
			fills = append(fills, snap.Fills...)
			if len(fills) > 200 {
				fills = fills[:200]
			}
		}
		if fills == nil {
			fills = []FillDTO{}
		}
		m.emit(wallet, "wallet.bootstrap.activity", fills, w.state)
	}
	// trades
	if w.channelEnabled("trades") {
		page := &ActivityPage{Rows: []ActivityTradeDTO{}, HasMore: false, Counts: ActivityCounts{}, DataStatus: DataError}
		if m.service != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			if p, err := m.service.Activity(ctx, DefaultVenue, wallet, ActivityQuery{Limit: 20, Sort: "closed_at", Dir: "desc"}); err == nil && p != nil {
				page = p
			}
			cancel()
		}
		if page.DataStatus == DataError && len(page.Rows) == 0 {
			// Do not emit bootstrap trades on error/empty (avoids overwriting good cache).
		} else {
			m.emit(wallet, "wallet.bootstrap.trades", page, w.state)
		}
	}
}

func channelForEvt(evt string) string {
	switch evt {
	case EvtPositionUpdated, "wallet.bootstrap.positions":
		return "positions"
	case EvtOrderCreated, EvtOrderUpdated, "wallet.bootstrap.orders":
		return "orders"
	case EvtFillCreated, EvtFundingCreated, EvtActivityCreated, "wallet.bootstrap.activity":
		return "activity"
	case "wallet.bootstrap.trades":
		return "trades"
	}
	return ""
}
