package hyperliquid

import (
	"context"
	"time"
)

// LiveClient is the interactive read-path HL client (LIVE-CONTRACT v1.2 §3).
//
// Separate from the sync client: OWN pacer with a small floor (~200ms),
// own 429 backoff (via the inner AdaptivePacer), bounded concurrency (~4)
// and per-request ctx timeouts. The sync client's 2s-floor pacer is untouched.
//
// All methods acquire the concurrency gate, then run with a per-request
// timeout. Callers treat ctx timeouts as truncated/partial (never hang).
type LiveClient struct {
	inner   *Client
	sem     chan struct{}
	timeout time.Duration
	// windowTimeout bounds multi-call window fetches (30d fills, funding).
	windowTimeout time.Duration
}

// Live defaults per contract §3: floor ~150-250ms (200ms), concurrency ~4,
// single-call timeout 12s, window timeout 25s.
const (
	LivePacerFloor      = 200 * time.Millisecond
	LiveConcurrency     = 4
	LiveSingleTimeout   = 12 * time.Second
	LiveWindowTimeout   = 25 * time.Second
	LiveCacheTTL        = 12 * time.Second
	LiveCacheTTLSeconds = 12
)

// NewLiveClient builds the interactive client. Zero values select the
// contract defaults (floor 200ms, concurrency 4, timeouts 12s/25s).
func NewLiveClient(baseURL string, httpTimeout, floor time.Duration, concurrency int) *LiveClient {
	if floor <= 0 {
		floor = LivePacerFloor
	}
	if concurrency <= 0 {
		concurrency = LiveConcurrency
	}
	if httpTimeout <= 0 {
		httpTimeout = LiveSingleTimeout
	}
	inner := NewClient(baseURL, httpTimeout, floor)
	return &LiveClient{
		inner:         inner,
		sem:           make(chan struct{}, concurrency),
		timeout:       httpTimeout,
		windowTimeout: LiveWindowTimeout,
	}
}

// WithWindowTimeout overrides the window-fetch bound (tests).
func (l *LiveClient) WithWindowTimeout(d time.Duration) *LiveClient {
	if d > 0 {
		l.windowTimeout = d
	}
	return l
}

// Interval reports the current pacer spacing (observability/tests).
func (l *LiveClient) Interval() time.Duration {
	if l == nil || l.inner == nil || l.inner.pacer == nil {
		return 0
	}
	return l.inner.pacer.Interval()
}

// Concurrency reports the gate size (tests/conformance).
func (l *LiveClient) Concurrency() int {
	if l == nil {
		return 0
	}
	return cap(l.sem)
}

func (l *LiveClient) acquire(ctx context.Context) (release func(), err error) {
	select {
	case l.sem <- struct{}{}:
		return func() { <-l.sem }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (l *LiveClient) singleCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, l.timeout)
}

func (l *LiveClient) windowCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, l.windowTimeout)
}

// FetchClearinghouseState implements the perp account summary (positions §1.1,
// balances §1.3) behind the concurrency gate + ctx timeout.
func (l *LiveClient) FetchClearinghouseState(ctx context.Context, address string) (*ClearinghouseState, error) {
	release, err := l.acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	ctx2, cancel := l.singleCtx(ctx)
	defer cancel()
	return l.inner.FetchClearinghouseState(ctx2, address)
}

// FetchSpotState implements spot balances (§1.3).
func (l *LiveClient) FetchSpotState(ctx context.Context, address string) (*SpotState, error) {
	release, err := l.acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	ctx2, cancel := l.singleCtx(ctx)
	defer cancel()
	return l.inner.FetchSpotState(ctx2, address)
}

// FetchUserFills implements recent fills (§1.4, 2000 most recent).
func (l *LiveClient) FetchUserFills(ctx context.Context, address string) ([]Fill, error) {
	release, err := l.acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	ctx2, cancel := l.singleCtx(ctx)
	defer cancel()
	return l.inner.FetchUserFills(ctx2, address)
}

// FetchOpenOrders implements open orders (§1.5).
func (l *LiveClient) FetchOpenOrders(ctx context.Context, address string) ([]OpenOrder, error) {
	release, err := l.acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	ctx2, cancel := l.singleCtx(ctx)
	defer cancel()
	return l.inner.FetchOpenOrders(ctx2, address)
}

// FetchHistoricalOrders implements historical orders (§1.5).
func (l *LiveClient) FetchHistoricalOrders(ctx context.Context, address string) ([]HistoricalOrder, error) {
	release, err := l.acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	ctx2, cancel := l.singleCtx(ctx)
	defer cancel()
	return l.inner.FetchHistoricalOrders(ctx2, address)
}

// FetchLedgerUpdates implements non-funding transfers (§1.6).
func (l *LiveClient) FetchLedgerUpdates(ctx context.Context, address string, startTimeMs int64) ([]LedgerUpdate, error) {
	release, err := l.acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	ctx2, cancel := l.singleCtx(ctx)
	defer cancel()
	return l.inner.FetchLedgerUpdates(ctx2, address, startTimeMs)
}

// FetchFillsWindow implements the 30d fills fetch for live activity (§1.2)
// and live performance (§1.7): userFillsByTime over [startMs, endMs] with the
// venue's subdivision + truncated flag. Bounded by the window timeout; ctx
// timeouts surface as errors and callers flag partial (never hang).
func (l *LiveClient) FetchFillsWindow(ctx context.Context, address string, startMs, endMs int64) ([]Fill, bool, error) {
	release, err := l.acquire(ctx)
	if err != nil {
		return nil, false, err
	}
	defer release()
	ctx2, cancel := l.windowCtx(ctx)
	defer cancel()
	return l.inner.FetchAll(ctx2, address, startMs, endMs)
}

// FetchUserFunding implements funding attribution (§1.2): userFunding history
// in [startMs, endMs]. Bounded by the window timeout.
func (l *LiveClient) FetchUserFunding(ctx context.Context, address string, startMs, endMs int64) ([]FundingUpdate, error) {
	release, err := l.acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	ctx2, cancel := l.windowCtx(ctx)
	defer cancel()
	return l.inner.FetchUserFunding(ctx2, address, startMs, endMs)
}

// FetchPortfolio implements live equity (§1.7).
func (l *LiveClient) FetchPortfolio(ctx context.Context, address string) (map[string][]PortfolioPoint, error) {
	release, err := l.acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	ctx2, cancel := l.singleCtx(ctx)
	defer cancel()
	return l.inner.FetchPortfolio(ctx2, address)
}

// FetchFillsWindow exposes the window fetch on the base client so both
// *Client and *LiveClient satisfy the extended live seam (the sync client
// keeps its 2s pacer; the live client gates + times out per §3).
func (c *Client) FetchFillsWindow(ctx context.Context, address string, startMs, endMs int64) ([]Fill, bool, error) {
	return c.FetchAll(ctx, address, startMs, endMs)
}
