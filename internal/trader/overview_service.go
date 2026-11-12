package trader

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

// Overview returns the Trader Overview aggregate.
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
	defer s.overviewMu.Unlock()

	if c, ok := s.overviewCache[cacheKey]; ok && time.Now().Before(c.exp) {
		v := cloneOverview(c.val)
		v.Venue = venue
		v.Period = period
		v.PnlMetric = pnlMetric
		return &v, nil
	}

	venueID, err := s.repo.VenueIDByCode(ctx, venue)
	if err != nil {
		return nil, domain.NewError(domain.ErrCodeInvalidFilter, "unknown venue")
	}

	pnlAggs, err := s.repo.AggregatePnLBuckets(ctx, venueID, period, pnlMetric)
	if err != nil {
		return nil, err
	}
	accountAggs, err := s.repo.AggregateAccountBuckets(ctx, venueID, period)
	if err != nil {
		return nil, err
	}
	universeSize, err := s.repo.UniverseSize(ctx, venueID, period)
	if err != nil {
		return nil, err
	}
	pnlWallets, err := s.repo.PnlWalletCount(ctx, venueID, period, pnlMetric)
	if err != nil {
		return nil, err
	}
	accountWallets, err := s.repo.AccountWalletCount(ctx, venueID)
	if err != nil {
		return nil, err
	}
	statusCounts, err := s.repo.DataStatusCounts(ctx, venueID, period)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	since := now.AddDate(0, 0, -seriesDays+1).Truncate(24 * time.Hour)
	seriesRows, err := s.repo.AggregateDailyPnLSeries(ctx, venueID, period, pnlMetric, since)
	if err != nil {
		return nil, err
	}
	seriesWallets, err := s.repo.SeriesWalletCount(ctx, venueID, period, pnlMetric, since)
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
		daily := map[time.Time]float64{}
		for _, row := range seriesRows {
			if row.Bucket != id {
				continue
			}
			daily[row.Date.UTC().Truncate(24*time.Hour)] += row.Pnl
		}
		series := CumulativeSeries(daily, since, now)
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
			Series:        series,
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

	acctSeriesRows, err := s.repo.AggregateAccountDailyPnLSeries(ctx, venueID, since)
	if err != nil {
		return nil, err
	}
	acctDaily := map[string]map[time.Time]float64{}
	for _, row := range acctSeriesRows {
		if acctDaily[row.Bucket] == nil {
			acctDaily[row.Bucket] = map[time.Time]float64{}
		}
		acctDaily[row.Bucket][row.Date.UTC().Truncate(24*time.Hour)] += row.Pnl
	}
	for i := range accountBuckets {
		id := accountBuckets[i].ID
		daily := acctDaily[id]
		if daily == nil {
			daily = map[time.Time]float64{}
		}
		accountBuckets[i].Series = CumulativeSeries(daily, since, now)
	}

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
			SeriesWallets:       seriesWallets,
			DataStatus:          statusCounts,
		},
	}

	s.overviewCache[cacheKey] = cachedOverview{val: result, exp: now.Add(5 * time.Minute)}
	return &result, nil
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
