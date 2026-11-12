package trader

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

func (s *Service) BucketDetail(ctx context.Context, venueCode, kind, id, period, rangeCode string) (*BucketDetail, error) {
	venue := strings.ToLower(strings.TrimSpace(venueCode))
	if venue == "" {
		venue = DefaultVenue
	}
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind != "pnl" && kind != "account" {
		return nil, domain.NewError(domain.ErrCodeInvalidFilter, "invalid kind")
	}
	id = strings.TrimSpace(id)
	if !ValidBucketID(kind, id) {
		return nil, domain.NewError(domain.ErrCodeInvalidFilter, "invalid bucket id")
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
	rangeCode = strings.ToUpper(strings.TrimSpace(rangeCode))
	if rangeCode == "" {
		rangeCode = "30D"
	}
	days, err := rangeDays(rangeCode)
	if err != nil {
		return nil, err
	}

	cacheKey := fmt.Sprintf("detail|%s|%s|%s|%s|%s", venue, kind, id, period, rangeCode)
	s.bucketDetailMu.Lock()
	defer s.bucketDetailMu.Unlock()
	if c, ok := s.bucketDetailCache[cacheKey]; ok && time.Now().Before(c.exp) {
		v := c.val
		v.Positioning = append([]BucketPositioningPoint(nil), c.val.Positioning...)
		v.MarketsTop = append([]BucketMarketTop(nil), c.val.MarketsTop...)
		return &v, nil
	}

	venueID, err := s.repo.VenueIDByCode(ctx, venue)
	if err != nil {
		return nil, domain.NewError(domain.ErrCodeInvalidFilter, "unknown venue")
	}
	wallets, err := s.repo.BucketWalletAddresses(ctx, venueID, kind, id, period)
	if err != nil {
		return nil, err
	}
	if len(wallets) == 0 {
		return nil, domain.NewError(domain.ErrCodeNotFound, "bucket not found")
	}

	lc, sc, usdProfit, usdLoss, volumeTotal, err := s.repo.BucketPeriodAgg(ctx, venueID, period, wallets)
	if err != nil {
		return nil, err
	}
	sentiment := Sentiment(lc, sc)
	longRatio := LongRatio(lc, sc)

	pctDenom := usdProfit + math.Abs(usdLoss)
	pctProfit, pctLoss := 0.0, 0.0
	if pctDenom > 0 {
		pctProfit = usdProfit / pctDenom
		pctLoss = math.Abs(usdLoss) / pctDenom
	}

	posLong, posShort, posWallets, err := s.repo.BucketPositionNotional(ctx, venueID, wallets)
	if err != nil {
		return nil, err
	}
	longUsd, shortUsd := posLong, posShort
	if posWallets == 0 {
		l, sh := ApproxNotional(volumeTotal, lc, sc)
		longUsd, shortUsd = l, sh
	}

	now := time.Now().UTC()
	start := now.AddDate(0, 0, -days+1).Truncate(24 * time.Hour)

	snapDays, err := s.repo.BucketPositioningSnapshots(ctx, venueID, wallets, start)
	if err != nil {
		return nil, err
	}
	useSnap := len(snapDays) > 0

	snapMap := map[time.Time][2]float64{}
	tradeMap := map[time.Time][2]float64{}
	statMap := map[time.Time][2]int64{}
	if useSnap {
		for _, d := range snapDays {
			snapMap[d.Date.UTC().Truncate(24*time.Hour)] = [2]float64{d.TradeLong, d.TradeShort}
		}
	} else {
		positioningDays, err := s.repo.BucketPositioning(ctx, venueID, wallets, start)
		if err != nil {
			return nil, err
		}
		for _, d := range positioningDays {
			tradeMap[d.Date.UTC().Truncate(24*time.Hour)] = [2]float64{d.TradeLong, d.TradeShort}
			statMap[d.Date.UTC().Truncate(24*time.Hour)] = [2]int64{d.StatLong, d.StatShort}
		}
	}

	positioning := make([]BucketPositioningPoint, 0, days)
	lastPct := 0.5
	for d := start; !d.After(now.Truncate(24 * time.Hour)); d = d.AddDate(0, 0, 1) {
		day := d.UTC().Truncate(24 * time.Hour)
		var pct float64
		if useSnap {
			if v, ok := snapMap[day]; ok {
				tl, ts := v[0], v[1]
				total := tl + ts
				if total > 0 {
					pct = tl / total
				} else {
					pct = lastPct
				}
			} else {
				pct = lastPct
			}
		} else {
			tl, ts := tradeMap[day][0], tradeMap[day][1]
			total := tl + ts
			if total > 0 {
				pct = tl / total
			} else {
				lc2, sc2 := statMap[day][0], statMap[day][1]
				tot := lc2 + sc2
				if tot > 0 {
					pct = float64(lc2) / float64(tot)
				} else {
					pct = lastPct
				}
			}
		}
		lastPct = pct
		positioning = append(positioning, BucketPositioningPoint{T: day.Format("2006-01-02"), LongPct: pct})
	}

	markets, _, err := s.repo.BucketMarkets(ctx, venueID, wallets, start, "", "notional", "desc", 1, 12)
	if err != nil {
		return nil, err
	}
	marketsTop := make([]BucketMarketTop, 0, len(markets))
	for _, m := range markets {
		marketsTop = append(marketsTop, BucketMarketTop{
			Market:    m.Market,
			Notional:  m.NotionalLong + m.NotionalShort,
			Upnl:      m.PnlProfit + m.PnlLoss,
			Sentiment: m.Sentiment,
			Traders:   m.TradersLong + m.TradersShort,
		})
	}

	posCount, tradeCount, err := s.repo.BucketCoverage(ctx, venueID, wallets)
	if err != nil {
		return nil, err
	}

	result := BucketDetail{
		AsOf:        now.Format(time.RFC3339),
		Kind:        kind,
		BucketID:    id,
		WalletCount: len(wallets),
		Sentiment:   sentiment,
		LongRatio:   longRatio,
		PnlSplit: BucketPnlSplit{
			PctProfit: pctProfit,
			PctLoss:   pctLoss,
			UsdProfit: usdProfit,
			UsdLoss:   usdLoss,
		},
		NotionalSplit: BucketNotionalSplit{
			LongUsd:  longUsd,
			ShortUsd: shortUsd,
		},
		Positioning: positioning,
		MarketsTop:  marketsTop,
		Coverage: BucketDetailCoverage{
			WalletsWithPositions: posCount,
			WalletsWithTrades:    tradeCount,
		},
	}
	cached := result
	cached.Positioning = append([]BucketPositioningPoint(nil), result.Positioning...)
	cached.MarketsTop = append([]BucketMarketTop(nil), result.MarketsTop...)
	s.bucketDetailCache[cacheKey] = cachedBucketDetail{val: cached, exp: now.Add(5 * time.Minute)}
	return &result, nil
}

func (s *Service) BucketWallets(ctx context.Context, venueCode, kind, id, period, sort, dir string, page, limit int) ([]BucketWalletRow, int, error) {
	venue := strings.ToLower(strings.TrimSpace(venueCode))
	if venue == "" {
		venue = DefaultVenue
	}
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind != "pnl" && kind != "account" {
		return nil, 0, domain.NewError(domain.ErrCodeInvalidFilter, "invalid kind")
	}
	id = strings.TrimSpace(id)
	if !ValidBucketID(kind, id) {
		return nil, 0, domain.NewError(domain.ErrCodeInvalidFilter, "invalid bucket id")
	}
	period = strings.ToUpper(strings.TrimSpace(period))
	if period == "" {
		period = Period30D
	}
	switch period {
	case Period1D, Period7D, Period30D, PeriodALL:
	default:
		return nil, 0, domain.NewError(domain.ErrCodeInvalidFilter, "invalid period")
	}
	sort = strings.ToLower(strings.TrimSpace(sort))
	if sort == "" {
		sort = "equity"
	}
	switch sort {
	case "equity", "win_rate", "volume", "pnl", "bias":
	default:
		return nil, 0, domain.NewError(domain.ErrCodeInvalidFilter, "invalid sort")
	}
	dir = strings.ToLower(strings.TrimSpace(dir))
	if dir == "" {
		dir = "desc"
	}
	if dir != "asc" && dir != "desc" {
		return nil, 0, domain.NewError(domain.ErrCodeInvalidFilter, "invalid dir")
	}
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		return nil, 0, domain.NewError(domain.ErrCodeInvalidFilter, "limit must be 1..100")
	}

	cacheKey := fmt.Sprintf("wallets|%s|%s|%s|%s|%s|%s|%d|%d", venue, kind, id, period, sort, dir, page, limit)
	s.bucketWalletsMu.Lock()
	defer s.bucketWalletsMu.Unlock()
	if c, ok := s.bucketWalletsCache[cacheKey]; ok && time.Now().Before(c.exp) {
		out := make([]BucketWalletRow, len(c.val))
		for i := range c.val {
			out[i] = c.val[i]
			out[i].DisplayName = cloneStringPtr(c.val[i].DisplayName)
		}
		return out, c.total, nil
	}

	venueID, err := s.repo.VenueIDByCode(ctx, venue)
	if err != nil {
		return nil, 0, domain.NewError(domain.ErrCodeInvalidFilter, "unknown venue")
	}
	wallets, err := s.repo.BucketWalletAddresses(ctx, venueID, kind, id, period)
	if err != nil {
		return nil, 0, err
	}
	if len(wallets) == 0 {
		return nil, 0, domain.NewError(domain.ErrCodeNotFound, "bucket not found")
	}

	rangeStart := periodRangeStart(period, time.Now().UTC())
	rows, total, err := s.repo.BucketWallets(ctx, venueID, kind, id, period, rangeStart, sort, dir, page, limit)
	if err != nil {
		return nil, 0, err
	}
	out := make([]BucketWalletRow, 0, len(rows))
	for _, r := range rows {
		bias := Sentiment(r.LongCount, r.ShortCount)
		cohort := AccountBucketFor(r.PerpEquity)
		out = append(out, BucketWalletRow{
			Address:       r.Address,
			DisplayName:   r.DisplayName,
			PerpEquity:    r.PerpEquity,
			PositionValue: r.PositionValue,
			Leverage:      r.Leverage,
			WinRate:       r.WinRate,
			VolumeRange:   r.VolumeRange,
			RealizedPnl:   r.RealizedPnl,
			Bias:          bias,
			Cohort:        cohort,
			LongCount:     r.LongCount,
			ShortCount:    r.ShortCount,
		})
	}
	cached := make([]BucketWalletRow, len(out))
	for i := range out {
		cached[i] = out[i]
		cached[i].DisplayName = cloneStringPtr(out[i].DisplayName)
	}
	s.bucketWalletsCache[cacheKey] = cachedBucketWallets{val: cached, total: total, exp: time.Now().UTC().Add(5 * time.Minute)}
	return out, total, nil
}

func (s *Service) BucketMarkets(ctx context.Context, venueCode, kind, id, period, rangeCode, search, sort, dir string, page, limit int) ([]BucketMarketRow, int, error) {
	venue := strings.ToLower(strings.TrimSpace(venueCode))
	if venue == "" {
		venue = DefaultVenue
	}
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind != "pnl" && kind != "account" {
		return nil, 0, domain.NewError(domain.ErrCodeInvalidFilter, "invalid kind")
	}
	id = strings.TrimSpace(id)
	if !ValidBucketID(kind, id) {
		return nil, 0, domain.NewError(domain.ErrCodeInvalidFilter, "invalid bucket id")
	}
	period = strings.ToUpper(strings.TrimSpace(period))
	if period == "" {
		period = Period30D
	}
	switch period {
	case Period1D, Period7D, Period30D, PeriodALL:
	default:
		return nil, 0, domain.NewError(domain.ErrCodeInvalidFilter, "invalid period")
	}
	rangeCode = strings.ToUpper(strings.TrimSpace(rangeCode))
	if rangeCode == "" {
		rangeCode = "30D"
	}
	days, err := rangeDays(rangeCode)
	if err != nil {
		return nil, 0, err
	}
	sort = strings.ToLower(strings.TrimSpace(sort))
	if sort == "" {
		sort = "notional"
	}
	switch sort {
	case "notional", "upnl", "traders":
	default:
		return nil, 0, domain.NewError(domain.ErrCodeInvalidFilter, "invalid sort")
	}
	dir = strings.ToLower(strings.TrimSpace(dir))
	if dir == "" {
		dir = "desc"
	}
	if dir != "asc" && dir != "desc" {
		return nil, 0, domain.NewError(domain.ErrCodeInvalidFilter, "invalid dir")
	}
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		return nil, 0, domain.NewError(domain.ErrCodeInvalidFilter, "limit must be 1..100")
	}

	cacheKey := fmt.Sprintf("markets|%s|%s|%s|%s|%s|%s|%s|%s|%d|%d", venue, kind, id, period, rangeCode, search, sort, dir, page, limit)
	s.bucketMarketsMu.Lock()
	defer s.bucketMarketsMu.Unlock()
	if c, ok := s.bucketMarketsCache[cacheKey]; ok && time.Now().Before(c.exp) {
		out := make([]BucketMarketRow, len(c.val))
		copy(out, c.val)
		return out, c.total, nil
	}

	venueID, err := s.repo.VenueIDByCode(ctx, venue)
	if err != nil {
		return nil, 0, domain.NewError(domain.ErrCodeInvalidFilter, "unknown venue")
	}
	wallets, err := s.repo.BucketWalletAddresses(ctx, venueID, kind, id, period)
	if err != nil {
		return nil, 0, err
	}
	if len(wallets) == 0 {
		return nil, 0, domain.NewError(domain.ErrCodeNotFound, "bucket not found")
	}
	rangeStart := time.Now().UTC().AddDate(0, 0, -days+1).Truncate(24 * time.Hour)
	rows, total, err := s.repo.BucketMarkets(ctx, venueID, wallets, rangeStart, search, sort, dir, page, limit)
	if err != nil {
		return nil, 0, err
	}
	out := make([]BucketMarketRow, len(rows))
	copy(out, rows)
	s.bucketMarketsCache[cacheKey] = cachedBucketMarkets{val: out, total: total, exp: time.Now().UTC().Add(5 * time.Minute)}
	return out, total, nil
}

func rangeDays(rangeCode string) (int, error) {
	switch rangeCode {
	case "24H":
		return 1, nil
	case "7D":
		return 7, nil
	case "30D":
		return 30, nil
	}
	return 0, domain.NewError(domain.ErrCodeInvalidFilter, "invalid range")
}

func periodRangeStart(period string, now time.Time) time.Time {
	switch period {
	case Period1D:
		return now.Add(-24 * time.Hour)
	case Period7D:
		return now.Add(-7 * 24 * time.Hour)
	case Period30D:
		return now.Add(-30 * 24 * time.Hour)
	case PeriodALL:
		return now.Add(-365 * 24 * time.Hour)
	}
	return now.Add(-30 * 24 * time.Hour)
}
