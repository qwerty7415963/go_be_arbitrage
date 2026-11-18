package trader

import (
	"context"
	"log"
	"time"

	"github.com/google/uuid"

	"golang.org/x/sync/singleflight"
)

// BucketProjector precomputes the bucket read model.
type BucketProjector struct {
	repo *Repository
	svc  *Service
	logf func(format string, args ...any)
	sf   singleflight.Group
}

func NewBucketProjector(repo *Repository, svc *Service) *BucketProjector {
	return &BucketProjector{repo: repo, svc: svc, logf: log.Printf}
}

func (p *BucketProjector) Refresh(ctx context.Context, venueID uuid.UUID) {
	if p == nil {
		return
	}
	_, _, _ = p.sf.Do(venueID.String(), func() (interface{}, error) {
		p.refreshOnce(ctx, venueID)
		return nil, nil
	})
}

func (p *BucketProjector) refreshOnce(ctx context.Context, venueID uuid.UUID) {
	start := time.Now().UTC()
	// advisory lock transaction
	tx, err := p.repo.pool.Begin(ctx)
	if err != nil {
		p.logf("projector: begin tx: %v", err)
		return
	}
	defer tx.Rollback(ctx)

	var acquired bool
	if err := tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(hashtext('trader_bucket_projector:' || $1::text))`, venueID).Scan(&acquired); err != nil {
		p.logf("projector: lock: %v", err)
		return
	}
	if !acquired {
		return
	}

	// watermark check
	var watermark time.Time
	row := tx.QueryRow(ctx, `
		SELECT GREATEST(
			COALESCE((SELECT MAX(updated_at) FROM trader_period_metrics WHERE venue_id = $1), '1970-01-01'::timestamptz),
			COALESCE((SELECT MAX(updated_at) FROM trader_equity_daily WHERE venue_id = $1), '1970-01-01'::timestamptz),
			COALESCE((SELECT MAX(updated_at) FROM trader_daily_stats WHERE venue_id = $1), '1970-01-01'::timestamptz),
			COALESCE((SELECT MAX(updated_at) FROM trader_positions WHERE venue_id = $1), '1970-01-01'::timestamptz),
			COALESCE((SELECT MAX(updated_at) FROM trader_trades WHERE venue_id = $1), '1970-01-01'::timestamptz),
			COALESCE((SELECT MAX(updated_at) FROM trader_registry WHERE venue_id = $1), '1970-01-01'::timestamptz)
		)`, venueID)
	if err := row.Scan(&watermark); err != nil {
		p.logf("projector: watermark: %v", err)
		return
	}

	state, err := p.repo.GetProjectorState(ctx, venueID)
	if err != nil {
		p.logf("projector: state: %v", err)
	}
	if state != nil && state.RefreshStatus == "ready" && !watermark.After(state.LastSourceWatermark) {
		// skip rebuild
		return
	}

	// update status running
	runningState := &BucketProjectorState{
		VenueID:             venueID,
		LastSourceWatermark: watermark,
		LastRefreshedAt:     time.Now().UTC(),
		RefreshStatus:       "running",
	}
	_ = p.repo.UpsertProjectorState(ctx, runningState)

	venueCode, err := p.repo.VenueCodeByID(ctx, venueID)
	if err != nil || venueCode == "" {
		venueCode = DefaultVenue
	}

	kinds := []string{"pnl", "account"}
	periods := []string{"1D", "7D", "30D", "ALL"}
	bucketIDs := map[string][]string{
		"pnl":     {"extremely_profitable", "very_profitable", "profitable", "unprofitable", "very_unprofitable", "rekt"},
		"account": {"apex", "whale", "large", "medium", "small"},
	}

	rebuildErr := func() error {
		for _, kind := range kinds {
			for _, bucketID := range bucketIDs[kind] {
				for _, period := range periods {
					// wallets
					wallets, err := p.repo.BucketWalletAddresses(ctx, venueID, kind, bucketID, period)
					if err != nil {
						return err
					}
					if len(wallets) == 0 {
						continue
					}
					// detail (default range 30D)
					detail, err := p.svc.computeBucketDetail(ctx, venueCode, kind, bucketID, period, "30D")
					if err != nil {
						return err
					}
					if err := p.repo.UpsertBucketDetail(ctx, venueID, kind, bucketID, period, "30D", detail, time.Now().UTC()); err != nil {
						return err
					}
					// members (full list via repo, page all)
					memberRows, _, err := p.repo.BucketWallets(ctx, venueID, kind, bucketID, period, periodRangeStart(period, time.Now().UTC()), "equity", "desc", 1, 1000)
					if err != nil {
						return err
					}
					// convert
					rm := make([]repoBucketWallet, 0, len(memberRows))
					for _, r := range memberRows {
						rm = append(rm, r)
					}
					if err := p.repo.UpsertBucketMembers(ctx, venueID, kind, bucketID, period, rm); err != nil {
						return err
					}
					// markets
					rangeStart := time.Now().UTC().AddDate(0, 0, -30+1).Truncate(24 * time.Hour)
					markets, _, _, err := p.repo.BucketMarkets(ctx, venueID, wallets, rangeStart, "", "notional", "desc", 1, 1000)
					if err != nil {
						return err
					}
					mr := make([]BucketMarketRow, len(markets))
					copy(mr, markets)
					p.svc.enrichBucketMarkets(ctx, venueID, mr)
					if err := p.repo.UpsertBucketMarkets(ctx, venueID, kind, bucketID, period, "30D", mr); err != nil {
						return err
					}
					// counts
					posCount, tradeCount, _ := p.repo.BucketCoverage(ctx, venueID, wallets)
					_ = posCount
					_ = tradeCount
					if err := p.repo.UpsertBucketCounts(ctx, venueID, kind, bucketID, period, len(wallets), len(markets)); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}()

	duration := int(time.Since(start).Milliseconds())
	if rebuildErr != nil {
		p.logf("projector: rebuild failed: %v", rebuildErr)
		errState := &BucketProjectorState{
			VenueID:             venueID,
			LastSourceWatermark: stateWatermark(state),
			LastRefreshedAt:     time.Now().UTC(),
			RefreshDurationMs:   duration,
			RefreshStatus:       "error",
		}
		msg := rebuildErr.Error()
		errState.LastError = &msg
		_ = p.repo.UpsertProjectorState(ctx, errState)
		return
	}

	readyState := &BucketProjectorState{
		VenueID:             venueID,
		LastSourceWatermark: watermark,
		LastRefreshedAt:     time.Now().UTC(),
		RefreshDurationMs:   duration,
		RefreshStatus:       "ready",
	}
	_ = p.repo.UpsertProjectorState(ctx, readyState)

	_ = tx.Commit(ctx)
}

func stateWatermark(s *BucketProjectorState) time.Time {
	if s == nil {
		return time.Unix(0, 0).UTC()
	}
	return s.LastSourceWatermark
}

var _ = singleflight.Group{}
