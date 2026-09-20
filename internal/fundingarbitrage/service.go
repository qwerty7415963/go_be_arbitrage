package fundingarbitrage

import (
	"context"
	"math"
	"time"

	"github.com/google/uuid"
)

const (
	staleThreshold = 15 * time.Minute
)

type Service struct {
	repo  *Repository
	cache *Cache
}

func NewService(repo *Repository, cache *Cache) *Service {
	return &Service{
		repo:  repo,
		cache: cache,
	}
}

// GetPerpVenues returns all venues that support perps
func (s *Service) GetPerpVenues(ctx context.Context) ([]VenuePerp, error) {
	return s.repo.GetPerpVenues(ctx)
}

// GetFundingArbitrage returns funding arbitrage opportunities for given venues
func (s *Service) GetFundingArbitrage(
	ctx context.Context,
	venueIDs []uuid.UUID,
	sortBy string,
	includeStale bool,
	forceRefresh bool,
) (*FundingArbitrageResponse, error) {
	// 1. Get venues info
	venues, err := s.repo.GetVenuesByIDs(ctx, venueIDs)
	if err != nil {
		return nil, err
	}
	venueMap := make(map[uuid.UUID]VenuePerp)
	for _, v := range venues {
		venueMap[v.ID] = v
	}

	// 2. Get venue-instrument mappings
	mappings, err := s.repo.GetVenueInstrumentMappings(ctx, venueIDs)
	if err != nil {
		return nil, err
	}

	// 3. Group by base_asset (instrument)
	tokenVenues := make(map[uuid.UUID]map[uuid.UUID]VenueInstrument)
	for _, m := range mappings {
		if tokenVenues[m.InstrumentID] == nil {
			tokenVenues[m.InstrumentID] = make(map[uuid.UUID]VenueInstrument)
		}
		tokenVenues[m.InstrumentID][m.VenueID] = m
	}

	// 4. Generate pairs
	var pairs []Pair
	for i := 0; i < len(venueIDs); i++ {
		for j := i + 1; j < len(venueIDs); j++ {
			venueA := venueMap[venueIDs[i]]
			venueB := venueMap[venueIDs[j]]

			pair := Pair{
				VenueA: VenueInfo{ID: venueA.ID, Code: venueA.Code, Name: venueA.Name},
				VenueB: VenueInfo{ID: venueB.ID, Code: venueB.Code, Name: venueB.Name},
			}

			// Try cache first
			if !forceRefresh {
				if tokens, ok := s.cache.Get(venueA.ID, venueB.ID); ok {
					pair.Tokens = tokens
					pairs = append(pairs, pair)
					continue
				}
			}

			// Find matching tokens for this pair
			var tokens []ArbitrageToken
			for instrumentID, venues := range tokenVenues {
				vA, hasA := venues[venueA.ID]
				vB, hasB := venues[venueB.ID]

				if !hasA || !hasB {
					continue
				}

				token := s.buildArbitrageToken(ctx, instrumentID, vA, vB, includeStale)
				if token != nil {
					tokens = append(tokens, *token)
				}
			}

			// Sort tokens
			SortTokens(tokens, sortBy)

			// Cache the result
			s.cache.Set(venueA.ID, venueB.ID, tokens)

			pair.Tokens = tokens
			pairs = append(pairs, pair)
		}
	}

	return &FundingArbitrageResponse{
		DataAsOf:    time.Now(),
		CacheStatus: "fresh",
		Pairs:       pairs,
	}, nil
}

func (s *Service) buildArbitrageToken(
	ctx context.Context,
	instrumentID uuid.UUID,
	vA, vB VenueInstrument,
	includeStale bool,
) *ArbitrageToken {
	// Get funding data for both venues
	fundingA, errA := s.repo.GetLatestFunding(ctx, vA.VenueID, instrumentID)
	fundingB, errB := s.repo.GetLatestFunding(ctx, vB.VenueID, instrumentID)

	// Build token even without funding
	token := &ArbitrageToken{
		InstrumentID:     instrumentID,
		Symbol:           vA.BaseAsset + "-PERP",
		VenueASymbol:     vA.VenueSymbol,
		VenueBSymbol:     vB.VenueSymbol,
		FundingAvailable: false,
		IsStale:          false,
	}

	// Check if both have funding
	if errA != nil || errB != nil {
		return token
	}

	// Check staleness
	isStaleA := time.Since(fundingA.ObservedAt) > staleThreshold
	isStaleB := time.Since(fundingB.ObservedAt) > staleThreshold

	if isStaleA || isStaleB {
		token.IsStale = true
		if !includeStale {
			return nil
		}
	}

	// Parse funding rates
	rateA := parseFundingRate(fundingA.FundingRate)
	rateB := parseFundingRate(fundingB.FundingRate)

	// Convert to hourly rates
	hourlyA := rateA * 3600 / float64(fundingA.IntervalSeconds)
	hourlyB := rateB * 3600 / float64(fundingB.IntervalSeconds)

	// Determine long/short direction
	// Long = venue with lower hourly rate (receives funding)
	// Short = venue with higher hourly rate (pays funding)
	var longVenueID, shortVenueID uuid.UUID
	var netHourly float64

	if hourlyA < hourlyB {
		longVenueID = vA.VenueID
		shortVenueID = vB.VenueID
		netHourly = hourlyB - hourlyA
	} else {
		longVenueID = vB.VenueID
		shortVenueID = vA.VenueID
		netHourly = hourlyA - hourlyB
	}

	token.LongVenueID = longVenueID
	token.ShortVenueID = shortVenueID
	token.VenueAFundingRate = fundingA.FundingRate
	token.VenueAIntervalSeconds = fundingA.IntervalSeconds
	token.VenueAObservedAt = fundingA.ObservedAt
	token.VenueAOI = fundingA.OpenInterest
	token.VenueBFundingRate = fundingB.FundingRate
	token.VenueBIntervalSeconds = fundingB.IntervalSeconds
	token.VenueBObservedAt = fundingB.ObservedAt
	token.VenueBOI = fundingB.OpenInterest
	token.FundingAvailable = true

	// Calculate price spread from mark_price
	markA := parseFundingRate(fundingA.MarkPrice)
	markB := parseFundingRate(fundingB.MarkPrice)
	if markA > 0 && markB > 0 {
		spread := math.Abs(markA-markB) / math.Min(markA, markB) * 100
		token.PriceSpreadPercent = &spread
	}

	// Calculate Rate 1h, Rate 8h, and APR from net hourly rate
	rate1h := netHourly * 100                     // % per hour
	rate8h := netHourly * 8 * 100                 // % per 8 hours
	apr := netHourly * 24 * 365 * 100             // annualized %
	token.Rate1hPercent = &rate1h
	token.Rate8hPercent = &rate8h
	token.APRPercent = &apr

	return token
}

func parseFundingRate(rate string) float64 {
	var r float64
	_, err := parseFloat(rate, &r)
	if err != nil {
		return 0
	}
	return r
}

func parseFloat(s string, f *float64) (int, error) {
	// Simple float parser
	var result float64
	var sign float64 = 1
	i := 0

	if len(s) == 0 {
		return 0, nil
	}

	if s[0] == '-' {
		sign = -1
		i = 1
	} else if s[0] == '+' {
		i = 1
	}

	for ; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			result = result*10 + float64(s[i]-'0')
		} else if s[i] == '.' {
			i++
			decimalPlace := 0.1
			for ; i < len(s); i++ {
				if s[i] >= '0' && s[i] <= '9' {
					result += float64(s[i]-'0') * decimalPlace
					decimalPlace *= 0.1
				}
			}
			break
		} else {
			break
		}
	}

	*f = result * sign
	return i, nil
}

// Abs returns the absolute value
func Abs(x float64) float64 {
	return math.Abs(x)
}
