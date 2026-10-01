package trader

import (
	"context"
	"log"
	"sort"
	"time"

	"github.com/google/uuid"
)

// DiscoveredWallet is one candidate from a venue discovery source
// (leaderboard row, WS trade party, manual seed). Venue-agnostic by design:
// later DEX venues implement DiscoveryFetcher the same way.
type DiscoveredWallet struct {
	Address      string
	DisplayName  *string
	AccountValue *float64
	// Windows maps upstream window name -> aggregates (e.g. day/week/month/allTime).
	Windows map[string]WindowStats
}

// WindowStats holds one window's upstream aggregates. ROI is a fraction.
type WindowStats struct {
	PnL    *float64
	ROI    *float64
	Volume *float64
}

// DiscoveryFetcher is the venue discovery seam. Implementations: leaderboard
// HTTP (V1), WS trade harvest (V1.1), static seed lists.
type DiscoveryFetcher interface {
	FetchTop(ctx context.Context, limit int) ([]DiscoveredWallet, error)
}

// DiscoveryResult counts one Discover run (observability, Phase 5 reads these).
type DiscoveryResult struct {
	Fetched  int
	Inserted int
	Updated  int
	Skipped  int
}

// DiscoveryService upserts discovered wallets into the registry plus their
// leaderboard window references (ROI passthrough source, spec D4).
type DiscoveryService struct {
	repo     *Repository
	fetch    DiscoveryFetcher
	venueID  uuid.UUID
	limit    int
	logf     func(format string, args ...any)
	counters discoveryCounters
}

func NewDiscoveryService(repo *Repository, fetch DiscoveryFetcher, venueID uuid.UUID, limit int) *DiscoveryService {
	return &DiscoveryService{repo: repo, fetch: fetch, venueID: venueID, limit: limit,
		logf: log.Printf}
}

// selectTop orders by month PnL desc (nil last) and caps at limit (<=0 = all).
func selectTop(rows []DiscoveredWallet, limit int) []DiscoveredWallet {
	out := append([]DiscoveredWallet(nil), rows...)
	sort.SliceStable(out, func(i, j int) bool {
		pi, pj := out[i].Windows["month"].PnL, out[j].Windows["month"].PnL
		if pi == nil {
			return false
		}
		if pj == nil {
			return true
		}
		return *pi > *pj
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// Discover runs one full pass: fetch → normalize → upsert registry + refs.
// Hard fetch errors abort before any write (BE-031: old data stays intact).
func (s *DiscoveryService) Discover(ctx context.Context) (*DiscoveryResult, error) {
	start := time.Now().UTC()
	rows, err := s.fetch.FetchTop(ctx, s.limit)
	if err != nil {
		s.recordRun(start, nil, err)
		return nil, err
	}
	res := &DiscoveryResult{Fetched: len(rows)}
	now := time.Now().UTC()
	for _, row := range selectTop(rows, s.limit) {
		addr, err := NormalizeAddress(row.Address)
		if err != nil {
			res.Skipped++
			continue
		}
		_, created, err := s.repo.UpsertRegistry(ctx, s.venueID, addr,
			SourceLeaderboard, row.DisplayName, nil)
		if err != nil {
			return res, err
		}
		if created {
			res.Inserted++
		} else {
			res.Updated++
		}
		for window, stats := range row.Windows {
			if _, ok := map[string]bool{"day": true, "week": true,
				"month": true, "allTime": true}[window]; !ok {
				continue
			}
			if err := s.repo.UpsertLeaderboardRef(ctx, &LeaderboardRef{
				VenueID: s.venueID, WalletAddress: addr, Window: window,
				PnL: stats.PnL, ROI: stats.ROI, Volume: stats.Volume,
				AccountValue: row.AccountValue, FetchedAt: now,
			}); err != nil {
				return res, err
			}
		}
	}
	s.logf("discovery venue=%s fetched=%d inserted=%d updated=%d skipped=%d",
		s.venueID, res.Fetched, res.Inserted, res.Updated, res.Skipped)
	s.recordRun(start, res, nil)
	return res, nil
}

// Start runs Discover immediately and on every interval until ctx ends.
func (s *DiscoveryService) Start(ctx context.Context, interval time.Duration) {
	run := func() {
		if _, err := s.Discover(ctx); err != nil {
			s.logf("discovery failed: %v", err)
		}
	}
	run()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}
