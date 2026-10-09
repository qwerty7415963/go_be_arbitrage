package trader

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
)

// SyncOptions tunes the sync engine. Zero values are replaced by
// DefaultSyncOptions().
type SyncOptions struct {
	// FullLookback bounds first-time backfills (ALL period = 365d).
	FullLookback time.Duration
	// PurgeRetention bounds raw fill staging (never retained permanently).
	// 60d covers day-recompute, incremental overlap and crash replay; daily
	// rows are the durable record.
	PurgeRetention time.Duration
	// StaleAfter marks period rows stale past this data age.
	StaleAfter time.Duration
	// LBRefFresh bounds leaderboard ROI passthrough freshness.
	LBRefFresh time.Duration
	// WalletTimeout bounds one wallet pass (fetch + compute).
	WalletTimeout time.Duration
	// Workers sizes the SyncAll pool (1 = sequential).
	Workers int
	// ColdAfter marks wallets without recent trades cold (slower cycle).
	ColdAfter time.Duration
	// ColdInterval is the minimum gap between cold-wallet passes.
	ColdInterval time.Duration
	// PortfolioInterval is the minimum gap between equity refreshes.
	PortfolioInterval time.Duration
	// LIVE-CONTRACT v1.2 WS-E: positions-30s sync removed. Detail positions
	// are LIVE (§1.1 REST, short TTL); the sync engine keeps daily/period for
	// the scanner only. PositionInterval/PositionColdInterval/PositionJitter
	// are retired (zero values ignored).
}

func DefaultSyncOptions() SyncOptions {
	return SyncOptions{
		FullLookback:      365 * 24 * time.Hour,
		PurgeRetention:    60 * 24 * time.Hour,
		StaleAfter:        24 * time.Hour,
		LBRefFresh:        24 * time.Hour,
		WalletTimeout:     10 * time.Minute,
		Workers:           1,
		ColdAfter:         7 * 24 * time.Hour,
		ColdInterval:      24 * time.Hour,
		PortfolioInterval: 24 * time.Hour,
	}
}

func (o *SyncOptions) withDefaults() SyncOptions {
	d := DefaultSyncOptions()
	if o.FullLookback > 0 {
		d.FullLookback = o.FullLookback
	}
	if o.PurgeRetention > 0 {
		d.PurgeRetention = o.PurgeRetention
	}
	if o.StaleAfter > 0 {
		d.StaleAfter = o.StaleAfter
	}
	if o.LBRefFresh > 0 {
		d.LBRefFresh = o.LBRefFresh
	}
	if o.WalletTimeout > 0 {
		d.WalletTimeout = o.WalletTimeout
	}
	if o.Workers > 0 {
		d.Workers = o.Workers
	}
	if o.ColdAfter > 0 {
		d.ColdAfter = o.ColdAfter
	}
	if o.ColdInterval > 0 {
		d.ColdInterval = o.ColdInterval
	}
	if o.PortfolioInterval > 0 {
		d.PortfolioInterval = o.PortfolioInterval
	}
	return d
}

// periodLookbacks maps search periods to aggregation windows.
var periodLookbacks = map[string]time.Duration{
	Period1D:  24 * time.Hour,
	Period7D:  7 * 24 * time.Hour,
	Period30D: 30 * 24 * time.Hour,
	PeriodALL: 365 * 24 * time.Hour,
}

// SyncService ingests venue fills per registry wallet and maintains daily
// aggregates + the period metric cache the scanner reads (LIVE-CONTRACT v1.2
// WS-E: scanner only; detail is LIVE and never reads sync tables). Work is
// recompute-based (never blind increments): reruns are identical (BE-020) and
// crash recovery only replays (BE-033).
type SyncService struct {
	repo      *Repository
	fetch     FillFetcher
	portfolio PortfolioFetcher // optional; nil skips equity (V1.1 wiring sets it)
	venueID   uuid.UUID
	opts      SyncOptions
	logf      func(format string, args ...any)
	counters  syncCounters
}

func NewSyncService(repo *Repository, fetch FillFetcher, venueID uuid.UUID, opts SyncOptions) *SyncService {
	return &SyncService{repo: repo, fetch: fetch, venueID: venueID,
		opts: opts.withDefaults(), logf: log.Printf}
}

// WithPortfolio enables the equity-curve job (V1.1); nil disables it.
func (s *SyncService) WithPortfolio(p PortfolioFetcher) *SyncService {
	s.portfolio = p
	return s
}

// SyncWallet runs one full pass for an address: fetch → stage → recompute
// affected days → recalc periods → advance cursor → purge. Fetch failures
// record error state (BE-010/BE-032) without touching last-good metrics
// beyond a status flip.
func (s *SyncService) SyncWallet(ctx context.Context, addr string, now time.Time) (err error) {
	ctx, cancel := context.WithTimeout(ctx, s.opts.WalletTimeout)
	defer cancel()

	// Every pass (success or failure) feeds the health counters.
	var fetchLatency time.Duration
	defer func() { s.recordWallet(fetchLatency, err) }()

	state, err := s.repo.EnsureSyncState(ctx, s.venueID, addr)
	if err != nil {
		return err
	}

	full := state.BackfillCompletedAt == nil
	start := now.Add(-s.opts.FullLookback)
	if !full && state.FillsLastTime != nil && state.FillsLastTime.After(start) {
		start = *state.FillsLastTime
	}
	if full {
		if err := s.repo.UpdateSyncProgress(ctx, s.venueID, addr,
			nil, nil, "syncing", nil, 0); err != nil {
			return err
		}
	}

	fetchStart := time.Now()
	fills, truncated, fetchErr := s.fetch.FetchTraderFills(ctx, addr,
		start.UnixMilli(), now.UnixMilli())
	fetchLatency = time.Since(fetchStart)
	if fetchErr != nil {
		// ctx may be expired (timeout): use a fresh one for state writes.
		bg, cancelBg := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancelBg()
		msg := fetchErr.Error()
		_ = s.repo.UpdateSyncProgress(bg, s.venueID, addr,
			nil, nil, "error", &msg, 1)
		s.logf("sync %s: fetch failed: %v", addr, fetchErr)
		// Flip period rows to error status, metrics vintage untouched.
		_ = s.recalcAll(bg, addr, now, truncated, state.LastFillsSyncAt, true)
		return fetchErr
	}

	if _, err := s.repo.UpsertBufferFills(ctx, s.venueID, addr, fills); err != nil {
		return err
	}

	if err := s.recomputeAffected(ctx, addr, fills, start, now); err != nil {
		return err
	}

	maxTime, maxTID := maxFillCursor(fills)
	cursorTime := now
	if !maxTime.IsZero() {
		cursorTime = maxTime
	}
	clearMsg := ""
	status := "ready"
	// Large negative delta resets the retry counter (GREATEST clamps at 0).
	if err := s.repo.UpdateSyncProgress(ctx, s.venueID, addr,
		&cursorTime, maxTID, status, &clearMsg, -state.RetryCount-10); err != nil {
		return err
	}
	// Mark fill-sync success time for staleness accounting.
	if _, err := s.repo.pool.Exec(ctx, `
		UPDATE trader_sync_state
		SET last_fills_sync_at = $3,
		    backfill_start_time = COALESCE(backfill_start_time, CASE WHEN $4 THEN $3 ELSE backfill_start_time END),
		    backfill_completed_at = CASE WHEN $4 THEN $3 ELSE backfill_completed_at END
		WHERE venue_id = $1 AND wallet_address = $2`,
		s.venueID, addr, now.UTC(), full); err != nil {
		return err
	}

	if s.portfolio != nil && (state.LastPortfolioSyncAt == nil ||
		now.Sub(*state.LastPortfolioSyncAt) >= s.opts.PortfolioInterval) {
		s.syncEquity(ctx, addr, now)
	}
	// WS-E: positions-30s sync + durable trades writes removed. Detail is
	// LIVE (§1.1 positions REST, §1.2 activity 30d); sync keeps daily/period
	// for the scanner only (no ReplaceTradesForDay, no PruneTrades, no
	// last_positions_sync_at writes here).
	if err := s.recalcAll(ctx, addr, now, truncated, &now, false); err != nil {
		return err
	}
	if _, err := s.repo.PurgeBuffer(ctx, s.venueID, addr, now.Add(-s.opts.PurgeRetention)); err != nil {
		return err
	}
	return nil
}

// syncEquity refreshes the daily equity curve from the portfolio endpoint.
// Failures are auxiliary: logged, counted in sync state, never failing the
// wallet pass (fills metrics are authoritative for readiness).
func (s *SyncService) syncEquity(ctx context.Context, addr string, now time.Time) {
	windows, err := s.portfolio.FetchPortfolio(ctx, addr)
	if err != nil {
		s.logf("sync %s: portfolio failed: %v", addr, err)
		return
	}
	var points []EquityPoint
	for _, w := range []string{"day", "week", "month", "allTime"} {
		points = append(points, windows[w]...)
	}
	cutoff := now.Add(-s.opts.PurgeRetention)
	for _, day := range AggregateEquityDaily(points) {
		if day.Date.Before(cutoff.Truncate(24*time.Hour)) || day.Date.After(now) {
			continue
		}
		if err := s.repo.UpsertEquityDaily(ctx, s.venueID, addr, day); err != nil {
			s.logf("sync %s: equity upsert failed: %v", addr, err)
			return
		}
	}
	_, _ = s.repo.pool.Exec(ctx, `
		UPDATE trader_sync_state SET last_portfolio_sync_at = $3, updated_at = NOW()
		WHERE venue_id = $1 AND wallet_address = $2`,
		s.venueID, addr, now.UTC())
}

// recomputeAffected rebuilds every day touched by this run from full buffer
// content (old + new fills), so cycles spanning days stay whole and reruns are
// identical. Days older than the purge cutoff are left untouched.
func (s *SyncService) recomputeAffected(ctx context.Context, addr string, fetched []Fill, start, now time.Time) error {
	cutoff := now.Add(-s.opts.PurgeRetention)
	daySet := map[string]bool{}
	for _, f := range fetched {
		if f.FilledAt.Before(cutoff) {
			continue
		}
		daySet[f.FilledAt.UTC().Truncate(24*time.Hour).Format("2006-01-02")] = true
	}
	// Reconstruction needs whole cycles: load the full buffered range once.
	all, err := s.repo.LoadBufferFills(ctx, s.venueID, addr, cutoff, now.Add(time.Second))
	if err != nil {
		return err
	}
	byCloseDay := map[string][]CompletedTrade{}
	for _, t := range ReconstructTrades(all) {
		if t.CloseTime.Before(cutoff) {
			continue
		}
		key := t.CloseTime.UTC().Truncate(24 * time.Hour).Format("2006-01-02")
		byCloseDay[key] = append(byCloseDay[key], t)
		daySet[key] = true
	}
	for key := range daySet {
		day, _ := time.Parse("2006-01-02", key)
		day = day.UTC()
		dayFills, err := s.repo.LoadBufferFills(ctx, s.venueID, addr, day, day.Add(24*time.Hour))
		if err != nil {
			return err
		}
		row := RollupDay("", addr, day, dayFills, byCloseDay[key])
		row.VenueID = s.venueID
		row.WalletAddress = addr
		if err := s.repo.UpsertDailyStats(ctx, row); err != nil {
			return err
		}
		// WS-E: durable trades writes removed (ReplaceTradesForDay calls in
		// the sync path). Detail activity is LIVE (§1.2 30d); sync keeps
		// daily/period for the scanner only.
	}
	return nil
}

// recalcAll rebuilds the four period rows from daily aggregates. On fetch
// failure the metrics vintage is preserved and only the status flips to error.
func (s *SyncService) recalcAll(ctx context.Context, addr string, now time.Time, partial bool, lastSuccess *time.Time, failed bool) error {
	status := DataReady
	asOf := now
	switch {
	case failed:
		status = DataError
		if lastSuccess != nil {
			asOf = *lastSuccess
		}
	case lastSuccess == nil:
		status = DataSyncing
	case now.Sub(*lastSuccess) > s.opts.StaleAfter:
		status = DataStale
		asOf = *lastSuccess
	}
	for _, period := range []string{Period1D, Period7D, Period30D, PeriodALL} {
		if err := s.recalcPeriod(ctx, addr, period, now, partial, status, asOf); err != nil {
			return err
		}
	}
	return nil
}

func (s *SyncService) recalcPeriod(ctx context.Context, addr, period string, now time.Time, partial bool, status DataStatus, asOf time.Time) error {
	from := now.Add(-periodLookbacks[period])
	days, err := s.repo.ListDailyStats(ctx, s.venueID, addr, from, now)
	if err != nil {
		return err
	}
	m := &PeriodMetrics{
		VenueID: s.venueID, WalletAddress: addr, Period: period,
		AsOf: asOf.UTC(), DataStatus: status, IsPartial: partial,
		CalculationVersion: CurrentCalculationVersion,
	}
	if len(days) == 0 {
		return s.repo.UpsertPeriodMetrics(ctx, m)
	}
	var trades, wins, losses int64
	var pnl, volume, gp, gl, holdSum float64
	var holdN int64
	var lastTrade *time.Time
	for _, d := range days {
		trades += d.TradeCount
		wins += d.WinCount
		losses += d.LossCount
		if d.RealizedPnL != nil {
			pnl += *d.RealizedPnL
		}
		if d.Volume != nil {
			volume += *d.Volume
		}
		if d.GrossProfit != nil {
			gp += *d.GrossProfit
		}
		if d.GrossLoss != nil {
			gl += *d.GrossLoss
		}
		holdSum += d.HoldingTimeSecSum
		holdN += d.HoldingTimeSecCount
		if d.LastTradeAt != nil && (lastTrade == nil || d.LastTradeAt.After(*lastTrade)) {
			t := *d.LastTradeAt
			lastTrade = &t
		}
	}
	tc := trades
	m.TradeCount = &tc
	// Display PnL: leaderboard passthrough when fresh, else the computed
	// realized sum (spec §9). RealizedPnL always carries the computed sum
	// for reconciliation.
	m.RealizedPnL = &pnl
	m.PnL = s.resolvePnL(ctx, addr, period, &pnl, now)
	m.Volume = &volume
	m.GrossProfit = &gp
	m.GrossLoss = &gl
	m.WinRate = WinRatePct(wins, losses)
	m.ProfitFactor = ProfitFactor(&gp, &gl)
	m.AvgTradePnL = AvgTradePnL(&pnl, trades)
	m.AvgHoldingTimeSec = AvgHoldingSec(holdSum, holdN)
	m.LastTradeAt = lastTrade
	var lw, sw, lc, sc int64
	for _, d := range days {
		lw += d.LongWins
		sw += d.ShortWins
		lc += d.LongCount
		sc += d.ShortCount
	}
	m.LongWins, m.ShortWins, m.LongCount, m.ShortCount = &lw, &sw, &lc, &sc
	m.ROI = s.resolveROI(ctx, addr, period, &pnl, &volume, now)
	if curve, err := s.repo.ListEquityDaily(ctx, s.venueID, addr, from, now); err == nil {
		m.MaxDrawdownPct = MaxDrawdownPct(curve)
	}
	return s.repo.UpsertPeriodMetrics(ctx, m)
}

// resolveROI applies the passthrough rule (spec v1.1 D4): fresh leaderboard
// window ROI wins; otherwise the documented v1 fallback estimate.
func (s *SyncService) resolveROI(ctx context.Context, addr, period string, pnl, volume *float64, now time.Time) *float64 {
	window, ok := LBWindowForPeriod[period]
	if ok {
		if ref, err := s.repo.GetLeaderboardRef(ctx, s.venueID, addr, window); err == nil &&
			ref != nil && ref.ROI != nil && now.Sub(ref.FetchedAt) <= s.opts.LBRefFresh {
			v := *ref.ROI * 100
			return &v
		}
	}
	return FallbackROIPct(pnl, volume)
}

// resolvePnL mirrors resolveROI for display PnL (spec §9: period PnL shown by
// the configured source). Fresh leaderboard window PnL wins; otherwise the
// computed realized sum.
func (s *SyncService) resolvePnL(ctx context.Context, addr, period string, realized *float64, now time.Time) *float64 {
	window, ok := LBWindowForPeriod[period]
	if ok {
		if ref, err := s.repo.GetLeaderboardRef(ctx, s.venueID, addr, window); err == nil &&
			ref != nil && ref.PnL != nil && now.Sub(ref.FetchedAt) <= s.opts.LBRefFresh {
			v := *ref.PnL
			return &v
		}
	}
	return realized
}

func maxFillCursor(fills []Fill) (time.Time, *int64) {
	var mt time.Time
	var tid *int64
	for _, f := range fills {
		if f.FilledAt.After(mt) || (f.FilledAt.Equal(mt) && (tid == nil || f.Tid > *tid)) {
			mt = f.FilledAt
			t := f.Tid
			tid = &t
		}
	}
	return mt, tid
}

// Sync tiers: pending wallets (never attempted) go first, hot wallets follow
// the scheduler tick, cold wallets wait out ColdInterval.
type syncTier int

const (
	tierPending syncTier = iota
	tierHot
	tierCold
)

// tierFor classifies a wallet: pending without a completed backfill, cold
// when its last trade is older than coldAfter (or unknown with a completed
// backfill), hot otherwise.
func tierFor(lastTrade *time.Time, backfillCompleted bool, now time.Time, coldAfter time.Duration) syncTier {
	if !backfillCompleted {
		return tierPending
	}
	if lastTrade == nil || now.Sub(*lastTrade) > coldAfter {
		return tierCold
	}
	return tierHot
}

// fanOut runs fn over items with at most workers goroutines, counting
// outcomes. Each item is processed exactly once (SYNC-U-01).
func fanOut(ctx context.Context, items []string, workers int, fn func(string) error) (done, failed int) {
	if workers < 1 {
		workers = 1
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	ch := make(chan string)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for addr := range ch {
				if err := fn(addr); err != nil {
					mu.Lock()
					failed++
					mu.Unlock()
					continue
				}
				mu.Lock()
				done++
				mu.Unlock()
			}
		}()
	}
	for _, addr := range items {
		select {
		case <-ctx.Done():
			break
		case ch <- addr:
		}
	}
	close(ch)
	wg.Wait()
	return done, failed
}

// SyncAll processes due wallets: pending first, hot on every tick, cold past
// ColdInterval, errored inside backoff skipped. Continues past per-wallet
// failures; returns (done, failed).
func (s *SyncService) SyncAll(ctx context.Context, now time.Time) (done, failed int) {
	start := time.Now().UTC()
	defer s.recordRun(start)
	items, err := s.repo.ListSyncQueue(ctx, s.venueID)
	if err != nil {
		s.logf("sync list: %v", err)
		return 0, 0
	}
	var due []string
	for _, it := range items {
		if it.Status == "error" && now.Before(it.UpdatedAt.Add(backoffDelay(it.RetryCount))) {
			continue // cooling down; retried by a later cycle
		}
		if it.LastSuccess == nil {
			due = append(due, it.Address) // pending or never succeeded
			continue
		}
		tier := tierFor(it.LastTradeAt, true, now, s.opts.ColdAfter)
		if tier == tierCold && now.Sub(*it.LastSuccess) < s.opts.ColdInterval {
			continue
		}
		due = append(due, it.Address)
	}
	done, failed = fanOut(ctx, due, s.opts.Workers, func(addr string) error {
		return s.SyncWallet(ctx, addr, now)
	})
	s.logf("sync finished venue=%s done=%d failed=%d duration_ms=%d",
		s.venueID, done, failed, time.Since(start).Milliseconds())
	return done, failed
}

// Start runs SyncAll immediately and on every interval until ctx ends.
func (s *SyncService) Start(ctx context.Context, interval time.Duration) {
	s.SyncAll(ctx, time.Now().UTC())
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.SyncAll(ctx, time.Now().UTC())
		}
	}
}
