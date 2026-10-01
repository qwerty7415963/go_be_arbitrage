package trader

import (
	"context"
	"log"
	"time"

	"github.com/google/uuid"
)

// SyncOptions tunes the sync engine. Zero values are replaced by
// DefaultSyncOptions().
type SyncOptions struct {
	// FullLookback bounds first-time backfills (ALL period = 365d).
	FullLookback time.Duration
	// PurgeRetention bounds raw fill staging (never retained permanently).
	PurgeRetention time.Duration
	// StaleAfter marks period rows stale past this data age.
	StaleAfter time.Duration
	// LBRefFresh bounds leaderboard ROI passthrough freshness.
	LBRefFresh time.Duration
	// WalletTimeout bounds one wallet pass (fetch + compute).
	WalletTimeout time.Duration
}

func DefaultSyncOptions() SyncOptions {
	return SyncOptions{
		FullLookback:   365 * 24 * time.Hour,
		PurgeRetention: 400 * 24 * time.Hour,
		StaleAfter:     24 * time.Hour,
		LBRefFresh:     24 * time.Hour,
		WalletTimeout:  10 * time.Minute,
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
// aggregates + the period metric cache the scanner reads. Work is
// recompute-based (never blind increments): reruns are identical (BE-020) and
// crash recovery only replays (BE-033).
type SyncService struct {
	repo    *Repository
	fetch   FillFetcher
	venueID uuid.UUID
	opts    SyncOptions
	logf    func(format string, args ...any)
}

func NewSyncService(repo *Repository, fetch FillFetcher, venueID uuid.UUID, opts SyncOptions) *SyncService {
	return &SyncService{repo: repo, fetch: fetch, venueID: venueID,
		opts: opts.withDefaults(), logf: log.Printf}
}

// SyncWallet runs one full pass for an address: fetch → stage → recompute
// affected days → recalc periods → advance cursor → purge. Fetch failures
// record error state (BE-010/BE-032) without touching last-good metrics
// beyond a status flip.
func (s *SyncService) SyncWallet(ctx context.Context, addr string, now time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, s.opts.WalletTimeout)
	defer cancel()

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

	fills, truncated, fetchErr := s.fetch.FetchTraderFills(ctx, addr,
		start.UnixMilli(), now.UnixMilli())
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

	if err := s.recalcAll(ctx, addr, now, truncated, &now, false); err != nil {
		return err
	}
	if _, err := s.repo.PurgeBuffer(ctx, s.venueID, addr, now.Add(-s.opts.PurgeRetention)); err != nil {
		return err
	}
	return nil
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
	m.PnL = &pnl
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

// SyncAll processes every active registry wallet sequentially, skipping
// errored wallets still inside their backoff window (BE-010). Continues past
// per-wallet failures; returns (done, failed).
func (s *SyncService) SyncAll(ctx context.Context, now time.Time) (done, failed int) {
	addrs, err := s.repo.ListRegistryAddresses(ctx, s.venueID)
	if err != nil {
		s.logf("sync list: %v", err)
		return 0, 0
	}
	for _, addr := range addrs {
		state, err := s.repo.GetSyncState(ctx, s.venueID, addr)
		if err == nil && state != nil && state.SyncStatus == "error" &&
			now.Before(state.UpdatedAt.Add(backoffDelay(state.RetryCount))) {
			continue // cooling down; retried by a later cycle
		}
		if err := s.SyncWallet(ctx, addr, now); err != nil {
			failed++
			continue
		}
		done++
	}
	s.logf("sync finished venue=%s done=%d failed=%d", s.venueID, done, failed)
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
