package trader

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

// Overview returns the Trader Overview aggregate (summary + buckets, no series).
func (s *Service) Overview(ctx context.Context, venueCode, period, pnlMetric string, seriesDays int) (*TraderOverview, error) {
	venue := strings.ToLower(strings.TrimSpace(venueCode))
	if venue == "" {
		venue = DefaultVenue
	}
	period = strings.ToUpper(strings.TrimSpace(period))
	if period == "" {
		period = Period30D
	}
	switch period {
	case Period1D, Period7D, Period30D, PeriodALL:
	default:
		return nil, domain.NewError(domain.ErrCodeInvalidFilter, "invalid period")
	}
	pnlMetric = strings.ToLower(strings.TrimSpace(pnlMetric))
	if pnlMetric == "" {
		pnlMetric = "realized_pnl"
	}
	if pnlMetric != "pnl" && pnlMetric != "realized_pnl" {
		return nil, domain.NewError(domain.ErrCodeInvalidFilter, "invalid pnl_metric")
	}
	if seriesDays < 7 || seriesDays > 90 {
		return nil, domain.NewError(domain.ErrCodeInvalidFilter, "series_days must be 7..90")
	}

	cacheKey := fmt.Sprintf("%s|%s|%s|%d", venue, period, pnlMetric, seriesDays)

	s.overviewMu.Lock()
	c, ok := s.overviewCache[cacheKey]
	expired := ok && time.Now().After(c.exp)
	s.overviewMu.Unlock()

	if ok && !expired {
		v := cloneOverview(c.val)
		v.Venue = venue
		v.Period = period
		v.PnlMetric = pnlMetric
		return &v, nil
	}

	if ok && expired {
		s.overviewMu.Lock()
		if !s.overviewCache[cacheKey].refreshing {
			updated := s.overviewCache[cacheKey]
			updated.refreshing = true
			s.overviewCache[cacheKey] = updated
			go func() {
				_, _, _ = s.overviewSF.Do(cacheKey, func() (any, error) {
					res, err := s.computeOverview(context.Background(), venue, period, pnlMetric, seriesDays)
					s.overviewMu.Lock()
					if err == nil && res != nil {
						s.overviewCache[cacheKey] = cachedOverview{val: *res, exp: time.Now().Add(15 * time.Minute), refreshing: false}
						vid, verr := s.repo.VenueIDByCode(context.Background(), venue)
						if verr == nil && vid != uuid.Nil {
							if payload, merr := json.Marshal(res); merr == nil {
								_ = s.repo.UpsertOverviewSnapshot(context.Background(), vid, period, pnlMetric, payload, time.Now().UTC())
							}
						}
					} else {
						cur := s.overviewCache[cacheKey]
						cur.refreshing = false
						s.overviewCache[cacheKey] = cur
					}
					s.overviewMu.Unlock()
					return res, err
				})
			}()
		}
		s.overviewMu.Unlock()

		v := cloneOverview(c.val)
		v.Venue = venue
		v.Period = period
		v.PnlMetric = pnlMetric
		return &v, nil
	}

	// L2 snapshot lookup
	venueID, _ := s.repo.VenueIDByCode(ctx, venue)
	if venueID != uuid.Nil {
		snap, serr := s.repo.GetOverviewSnapshot(ctx, venueID, period, pnlMetric)
		if serr == nil && snap != nil {
			threshold := time.Now().Add(-240 * time.Second)
			if snap.ComputedAt.After(threshold) {
				var ov TraderOverview
				if uerr := json.Unmarshal(snap.Payload, &ov); uerr == nil {
					s.overviewMu.Lock()
					s.overviewCache[cacheKey] = cachedOverview{val: ov, exp: time.Now().Add(15 * time.Minute), refreshing: false}
					s.overviewMu.Unlock()
					out := cloneOverview(ov)
					out.Venue = venue
					out.Period = period
					out.PnlMetric = pnlMetric
					return &out, nil
				}
			}
		}
	}

	// L3 live compute
	v, err, _ := s.overviewSF.Do(cacheKey, func() (any, error) {
		return s.computeOverview(ctx, venue, period, pnlMetric, seriesDays)
	})
	if err != nil {
		return nil, err
	}
	res := v.(*TraderOverview)
	s.overviewMu.Lock()
	s.overviewCache[cacheKey] = cachedOverview{val: *res, exp: time.Now().Add(15 * time.Minute), refreshing: false}
	s.overviewMu.Unlock()

	if venueID == uuid.Nil {
		venueID, _ = s.repo.VenueIDByCode(ctx, venue)
	}
	if venueID != uuid.Nil {
		if payload, merr := json.Marshal(res); merr == nil {
			_ = s.repo.UpsertOverviewSnapshot(ctx, venueID, period, pnlMetric, payload, time.Now().UTC())
		}
	}

	out := cloneOverview(*res)
	out.Venue = venue
	out.Period = period
	out.PnlMetric = pnlMetric
	return &out, nil
}

// OverviewSeries returns the daily series for the overview.
func (s *Service) OverviewSeries(ctx context.Context, venueCode, period, pnlMetric string, seriesDays int) (*OverviewSeries, error) {
	venue := strings.ToLower(strings.TrimSpace(venueCode))
	if venue == "" {
		venue = DefaultVenue
	}
	period = strings.ToUpper(strings.TrimSpace(period))
	if period == "" {
		period = Period30D
	}
	switch period {
	case Period1D, Period7D, Period30D, PeriodALL:
	default:
		return nil, domain.NewError(domain.ErrCodeInvalidFilter, "invalid period")
	}
	pnlMetric = strings.ToLower(strings.TrimSpace(pnlMetric))
	if pnlMetric == "" {
		pnlMetric = "realized_pnl"
	}
	if pnlMetric != "pnl" && pnlMetric != "realized_pnl" {
		return nil, domain.NewError(domain.ErrCodeInvalidFilter, "invalid pnl_metric")
	}
	if seriesDays < 7 || seriesDays > 90 {
		return nil, domain.NewError(domain.ErrCodeInvalidFilter, "series_days must be 7..90")
	}

	venueID, err := s.repo.VenueIDByCode(ctx, venue)
	if err != nil {
		return nil, domain.NewError(domain.ErrCodeInvalidFilter, "unknown venue")
	}

	now := time.Now().UTC()
	since := now.AddDate(0, 0, -seriesDays+1).Truncate(24 * time.Hour)
	pnlRows, acctRows, seriesWallets, err := s.repo.AggregateOverviewSeries(ctx, venueID, period, pnlMetric, since)
	if err != nil {
		return nil, err
	}

	result := &OverviewSeries{
		Venue:         venue,
		Period:        period,
		PnlMetric:     pnlMetric,
		SeriesDays:    seriesDays,
		SeriesWallets: seriesWallets,
		PnlSeries:     pnlRows,
		AccountSeries: acctRows,
	}
	if result.PnlSeries == nil {
		result.PnlSeries = []DailyPnLSeriesRow{}
	}
	if result.AccountSeries == nil {
		result.AccountSeries = []DailyPnLSeriesRow{}
	}
	return result, nil
}

func (s *Service) computeOverview(ctx context.Context, venue, period, pnlMetric string, seriesDays int) (*TraderOverview, error) {
	venueID, err := s.repo.VenueIDByCode(ctx, venue)
	if err != nil {
		return nil, domain.NewError(domain.ErrCodeInvalidFilter, "unknown venue")
	}

	pnlAggs, universeSize, pnlWallets, statusCounts, err := s.repo.AggregateOverviewCore(ctx, venueID, period, pnlMetric)
	if err != nil {
		return nil, err
	}
	accountAggs, accountWallets, err := s.repo.AggregateAccountBuckets(ctx, venueID, period)
	if err != nil {
		return nil, err
	}

	pnlMap := map[string]PnLBucketAgg{}
	for _, a := range pnlAggs {
		pnlMap[a.Bucket] = a
	}
	pnlBuckets := make([]OverviewBucket, 0, len(PnLBucketOrder))
	for _, id := range PnLBucketOrder {
		agg := pnlMap[id]
		var spec PnLBucketSpec
		for _, sp := range PnLBucketSpecs {
			if sp.ID == id {
				spec = sp
				break
			}
		}
		lr := LongRatio(agg.LongCount, agg.ShortCount)
		ln, sn := ApproxNotional(agg.VolumeSum, agg.LongCount, agg.ShortCount)
		pnlBuckets = append(pnlBuckets, OverviewBucket{
			ID:            id,
			MinPnL:        cloneFloatPtr(spec.Min),
			MaxPnL:        cloneFloatPtr(spec.Max),
			WalletCount:   agg.Count,
			PnlTotal:      agg.PnlSum,
			VolumeTotal:   agg.VolumeSum,
			LongCount:     agg.LongCount,
			ShortCount:    agg.ShortCount,
			LongRatio:     lr,
			LongNotional:  ln,
			ShortNotional: sn,
			Sentiment:     Sentiment(agg.LongCount, agg.ShortCount),
			Series:        []OverviewPoint{},
		})
	}

	accountMap := map[string]AccountBucketAgg{}
	for _, a := range accountAggs {
		accountMap[a.Bucket] = a
	}
	accountBuckets := make([]OverviewAccountBucket, 0, len(AccountBucketOrder))
	for _, id := range AccountBucketOrder {
		agg := accountMap[id]
		var spec AccountBucketSpec
		for _, sp := range AccountBucketSpecs {
			if sp.ID == id {
				spec = sp
				break
			}
		}
		ratio := LongRatio(agg.LongCount, agg.ShortCount)
		longN, shortN := ApproxNotional(agg.VolumeSum, agg.LongCount, agg.ShortCount)
		accountBuckets = append(accountBuckets, OverviewAccountBucket{
			ID:            id,
			MinEquity:     cloneFloatPtr(spec.Min),
			MaxEquity:     cloneFloatPtr(spec.Max),
			WalletCount:   agg.Count,
			EquityTotal:   agg.EquitySum,
			VolumeTotal:   agg.VolumeSum,
			LongCount:     agg.LongCount,
			ShortCount:    agg.ShortCount,
			LongRatio:     ratio,
			LongNotional:  longN,
			ShortNotional: shortN,
			Sentiment:     Sentiment(agg.LongCount, agg.ShortCount),
			Series:        []OverviewPoint{},
		})
	}

	now := time.Now().UTC()
	result := TraderOverview{
		AsOf:           now.Format(time.RFC3339),
		Venue:          venue,
		Period:         period,
		PnlMetric:      pnlMetric,
		UniverseSize:   universeSize,
		PnlBuckets:     pnlBuckets,
		AccountBuckets: accountBuckets,
		Coverage: OverviewCoverage{
			PnlWallets:          pnlWallets,
			AccountValueWallets: accountWallets,
			SeriesWallets:       0,
			DataStatus:          statusCounts,
		},
	}

	return &result, nil
}

// RefreshOverviewSnapshots recomputes and upserts all 8 overview snapshots for a venue.
func (s *Service) RefreshOverviewSnapshots(ctx context.Context, venueID uuid.UUID) {
	venueCode, err := s.repo.VenueCodeByID(ctx, venueID)
	if err != nil || venueCode == "" {
		venueCode = DefaultVenue
	}
	periods := []string{Period1D, Period7D, Period30D, PeriodALL}
	metrics := []string{"pnl", "realized_pnl"}
	for _, period := range periods {
		for _, metric := range metrics {
			res, err := s.computeOverview(ctx, venueCode, period, metric, 30)
			if err != nil || res == nil {
				continue
			}
			payload, merr := json.Marshal(res)
			if merr != nil {
				continue
			}
			_ = s.repo.UpsertOverviewSnapshot(ctx, venueID, period, metric, payload, time.Now().UTC())
			cacheKey := fmt.Sprintf("%s|%s|%s|%d", venueCode, period, metric, 30)
			s.overviewMu.Lock()
			s.overviewCache[cacheKey] = cachedOverview{val: *res, exp: time.Now().Add(15 * time.Minute), refreshing: false}
			s.overviewMu.Unlock()
		}
	}
}

// WarmOverviewCache computes all 8 overview keys in the background.
func (s *Service) WarmOverviewCache(ctx context.Context) {
	venueID, err := s.repo.VenueIDByCode(ctx, DefaultVenue)
	if err != nil {
		return
	}
	s.RefreshOverviewSnapshots(ctx, venueID)
}

func cloneStringPtr(p *string) *string {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func cloneFloatPtr(p *float64) *float64 {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func cloneOverview(o TraderOverview) TraderOverview {
	o.PnlBuckets = clonePnLBuckets(o.PnlBuckets)
	o.AccountBuckets = cloneAccountBuckets(o.AccountBuckets)
	if o.Coverage.DataStatus != nil {
		m := make(map[string]int, len(o.Coverage.DataStatus))
		for k, v := range o.Coverage.DataStatus {
			m[k] = v
		}
		o.Coverage.DataStatus = m
	}
	return o
}

func clonePnLBuckets(in []OverviewBucket) []OverviewBucket {
	out := make([]OverviewBucket, len(in))
	copy(out, in)
	for i := range out {
		out[i].MinPnL = cloneFloatPtr(in[i].MinPnL)
		out[i].MaxPnL = cloneFloatPtr(in[i].MaxPnL)
		out[i].Series = append([]OverviewPoint(nil), in[i].Series...)
	}
	return out
}

func cloneAccountBuckets(in []OverviewAccountBucket) []OverviewAccountBucket {
	out := make([]OverviewAccountBucket, len(in))
	copy(out, in)
	for i := range out {
		out[i].MinEquity = cloneFloatPtr(in[i].MinEquity)
		out[i].MaxEquity = cloneFloatPtr(in[i].MaxEquity)
		out[i].Series = append([]OverviewPoint(nil), in[i].Series...)
	}
	return out
}
