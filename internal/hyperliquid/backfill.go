package hyperliquid

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/wallet"
)

// VenueCode is the venues.code row seeded by migration 000015.
const VenueCode = "hyperliquid"

// VenueClient fetches raw venue fills; *Client implements it, tests inject
// a fake.
type VenueClient interface {
	FetchAll(ctx context.Context, address string, startMs, endMs int64) ([]Fill, bool, error)
}

type backfillWindow struct {
	timeframe string
	lookback  time.Duration
}

var backfillWindows = []backfillWindow{
	{wallet.Timeframe24H, 24 * time.Hour},
	{wallet.Timeframe7D, 7 * 24 * time.Hour},
	{wallet.Timeframe30D, 30 * 24 * time.Hour},
	{wallet.Timeframe90D, 90 * 24 * time.Hour},
	{wallet.TimeframeALL, 365 * 24 * time.Hour},
}

// BackfillService ingests venue fills per tracked wallet and writes metric
// snapshots (aggregate + per-market) through the Phase 2 engine.
type BackfillService struct {
	fills   *wallet.FillRepository
	client  VenueClient
	venueID uuid.UUID
	logf    func(format string, args ...any)
}

func NewBackfillService(fills *wallet.FillRepository, client VenueClient, venueID uuid.UUID) *BackfillService {
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

	raw, truncated, err := s.client.FetchAll(ctx, address, start.UnixMilli(), end.UnixMilli())
	if err != nil {
		return err
	}
	inputs, skipped := NormalizeFills(raw)
	if skipped > 0 {
		s.logf("hyperliquid backfill %s %s: skipped %d unparseable fills", address, w.timeframe, skipped)
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
	metrics := wallet.ComputeMetrics(toMetricFills(stored), wallet.MetricWindow{Start: start, End: end})
	if err := s.fills.UpsertSnapshot(ctx, walletID, w.timeframe, "", metrics, truncated); err != nil {
		return err
	}

	// Per-market snapshots so the scanner market filter matches real data.
	byMarket := map[string][]wallet.MetricFill{}
	for _, p := range stored {
		byMarket[p.Market] = append(byMarket[p.Market], p.MetricFill)
	}
	for market, mf := range byMarket {
		mm := wallet.ComputeMetrics(mf, wallet.MetricWindow{Start: start, End: end})
		if err := s.fills.UpsertSnapshot(ctx, walletID, w.timeframe, market, mm, truncated); err != nil {
			return err
		}
	}
	return nil
}

func toMetricFills(stored []wallet.PersistedFill) []wallet.MetricFill {
	out := make([]wallet.MetricFill, 0, len(stored))
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
			s.logf("hyperliquid backfill %s: %v", w.Address, err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		done++
	}
	return done, failed, firstErr
}
