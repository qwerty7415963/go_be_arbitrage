package wallet

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
)

// FillFetcher is the ingestion seam every venue adapter implements: fetch
// normalized fills for an address in [startMs, endMs] (ms since epoch).
// truncated=true means the venue capped the window (older data hidden) and
// the resulting snapshots must be flagged partial. Venue adapters normalize
// their native payloads to FillInput; everything downstream (persist, legs,
// snapshots, scanner) is venue-blind.
type FillFetcher interface {
	FetchFills(ctx context.Context, address string, startMs, endMs int64) ([]FillInput, bool, error)
}

type backfillWindow struct {
	timeframe string
	lookback  time.Duration
}

var backfillWindows = []backfillWindow{
	{Timeframe24H, 24 * time.Hour},
	{Timeframe7D, 7 * 24 * time.Hour},
	{Timeframe30D, 30 * 24 * time.Hour},
	{Timeframe90D, 90 * 24 * time.Hour},
	{TimeframeALL, 365 * 24 * time.Hour},
}

// BackfillService ingests venue fills per tracked wallet and writes metric
// snapshots (aggregate + per-market) through the engine. One instance serves
// one venue (venueID); add one instance per venue to support more.
type BackfillService struct {
	fills   *FillRepository
	client  FillFetcher
	venueID uuid.UUID
	logf    func(format string, args ...any)
}

func NewBackfillService(fills *FillRepository, client FillFetcher, venueID uuid.UUID) *BackfillService {
	return &BackfillService{
		fills:   fills,
		client:  client,
		venueID: venueID,
		logf:    log.Printf,
	}
}

// BackfillWallet fetches, persists and snapshots one wallet for every
// timeframe window ending at now (ING-I-01).
func (s *BackfillService) BackfillWallet(ctx context.Context, walletID uuid.UUID, address string, now time.Time) error {
	for _, w := range backfillWindows {
		if err := s.backfillWindow(ctx, walletID, address, w, now); err != nil {
			return fmt.Errorf("backfill %s %s: %w", address, w.timeframe, err)
		}
	}
	return nil
}

func (s *BackfillService) backfillWindow(ctx context.Context, walletID uuid.UUID, address string, w backfillWindow, now time.Time) error {
	start := now.Add(-w.lookback).UTC()
	end := now.UTC()

	inputs, truncated, err := s.client.FetchFills(ctx, address, start.UnixMilli(), end.UnixMilli())
	if err != nil {
		return err
	}

	if _, err := s.fills.UpsertFills(ctx, walletID, s.venueID, inputs); err != nil {
		return err
	}
	// Leg indexes are fetch-local; recompute globally so grouping stays
	// stable as wider windows reveal older fills.
	if _, err := s.fills.RecomputeLegs(ctx, walletID); err != nil {
		return err
	}

	stored, err := s.fills.LoadFills(ctx, walletID, start, end)
	if err != nil {
		return err
	}

	// Aggregate snapshot over the whole window.
	metrics := ComputeMetrics(toMetricFills(stored), MetricWindow{Start: start, End: end})
	if err := s.fills.UpsertSnapshot(ctx, walletID, w.timeframe, "", metrics, truncated); err != nil {
		return err
	}

	// Per-market snapshots so the scanner market filter matches real data.
	byMarket := map[string][]MetricFill{}
	for _, p := range stored {
		byMarket[p.Market] = append(byMarket[p.Market], p.MetricFill)
	}
	for market, mf := range byMarket {
		mm := ComputeMetrics(mf, MetricWindow{Start: start, End: end})
		if err := s.fills.UpsertSnapshot(ctx, walletID, w.timeframe, market, mm, truncated); err != nil {
			return err
		}
	}
	return nil
}

func toMetricFills(stored []PersistedFill) []MetricFill {
	out := make([]MetricFill, 0, len(stored))
	for _, p := range stored {
		out = append(out, p.MetricFill)
	}
	return out
}

// BackfillAll processes every EVM tracked wallet, continuing past per-wallet
// failures. Returns counts and the first error encountered (if any).
func (s *BackfillService) BackfillAll(ctx context.Context, now time.Time) (done, failed int, err error) {
	wallets, err := s.fills.BackfillWallets(ctx)
	if err != nil {
		return 0, 0, err
	}
	var firstErr error
	for _, w := range wallets {
		if err := s.BackfillWallet(ctx, w.ID, w.Address, now); err != nil {
			failed++
			s.logf("backfill %s: %v", w.Address, err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		done++
	}
	return done, failed, firstErr
}
