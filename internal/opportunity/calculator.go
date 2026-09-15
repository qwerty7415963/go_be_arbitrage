package opportunity

import (
	"math/big"
	"time"

	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/unifiedstate"
)

type Calculator struct {
	config ScannerConfig
}

func NewCalculator(config ScannerConfig) *Calculator {
	return &Calculator{config: config}
}

type PriceArbInput struct {
	InstrumentID    uuid.UUID
	CanonicalSymbol string
	BuyVenue        *unifiedstate.VenueMarketState
	SellVenue       *unifiedstate.VenueMarketState
	BuyVenueID      uuid.UUID
	BuyVenueCode    string
	SellVenueID     uuid.UUID
	SellVenueCode   string
}

type FundingArbInput struct {
	InstrumentID    uuid.UUID
	CanonicalSymbol string
	VenueA          *unifiedstate.VenueMarketState
	VenueB          *unifiedstate.VenueMarketState
	VenueAID        uuid.UUID
	VenueACode      string
	VenueBID        uuid.UUID
	VenueBCode      string
}

type BasisArbInput struct {
	InstrumentID    uuid.UUID
	CanonicalSymbol string
	Venue           *unifiedstate.VenueMarketState
	VenueID         uuid.UUID
	VenueCode       string
}

func (c *Calculator) CalculatePriceArb(input *PriceArbInput) (*Opportunity, error) {
	if input.BuyVenue == nil || input.SellVenue == nil {
		return nil, ErrVenueNotHealthy
	}

	if input.BuyVenue.Health != unifiedstate.VenueHealthHealthy ||
		input.SellVenue.Health != unifiedstate.VenueHealthHealthy {
		return nil, ErrVenueNotHealthy
	}

	if input.BuyVenue.BestAsk == nil || input.SellVenue.BestBid == nil {
		return nil, ErrInsufficientDepth
	}

	buyPrice := parseFloat(input.BuyVenue.BestAsk)
	sellPrice := parseFloat(input.SellVenue.BestBid)

	if buyPrice <= 0 || sellPrice <= 0 {
		return nil, ErrInvalidPrice
	}

	if sellPrice <= buyPrice {
		return nil, ErrBelowMinEdge
	}

	grossEdge := sellPrice - buyPrice
	grossEdgeBPS := int((grossEdge / buyPrice) * 10000)

	if grossEdgeBPS < c.config.MinGrossEdgeBPS {
		return nil, ErrBelowMinEdge
	}

	buyFee := c.getFeeModel(input.BuyVenueCode)
	sellFee := c.getFeeModel(input.SellVenueCode)

	totalFeeBPS := buyFee.TakerFeeBPS + sellFee.TakerFeeBPS
	feeAmount := buyPrice * float64(totalFeeBPS) / 10000.0

	slippageBPS := c.estimateSlippage(input.BuyVenue, input.SellVenue)
	slippageAmount := buyPrice * float64(slippageBPS) / 10000.0

	netEdge := grossEdge - feeAmount - slippageAmount
	netEdgeBPS := int((netEdge / buyPrice) * 10000)

	if netEdgeBPS < c.config.MinNetEdgeBPS {
		return nil, ErrBelowMinEdge
	}

	size, notional := c.calculateSizing(buyPrice, netEdge)

	confidence := c.calculateConfidence(
		input.BuyVenue,
		input.SellVenue,
		grossEdgeBPS,
		netEdgeBPS,
	)

	if confidence < c.config.MinConfidence {
		return nil, ErrBelowMinConfidence
	}

	quality := c.assessMarketQuality(input.BuyVenue, input.SellVenue)

	now := time.Now()
	oppID := uuid.New()

	opp := &Opportunity{
		ID:                  oppID,
		OpportunityType:     OpportunityTypePriceArb,
		Status:              OpportunityStatusDetected,
		InstrumentID:        input.InstrumentID,
		CanonicalSymbol:     input.CanonicalSymbol,
		DetectedAt:          now,
		ExpiresAt:           now.Add(c.config.OpportunityTTL),
		BuyVenueID:          input.BuyVenueID,
		BuyVenueCode:        input.BuyVenueCode,
		SellVenueID:         input.SellVenueID,
		SellVenueCode:       input.SellVenueCode,
		GrossEdge:           formatFloat(grossEdge),
		GrossEdgeBPS:        grossEdgeBPS,
		EstimatedFees:       formatFloat(feeAmount),
		EstimatedSlippage:   formatFloat(slippageAmount),
		EstimatedOtherCosts: "0",
		ExpectedNetEdge:     formatFloat(netEdge),
		ExpectedNetEdgeBPS:  netEdgeBPS,
		ExpectedNetPnl:      formatFloat(netEdge * size),
		SuggestedSize:       formatFloat(size),
		SuggestedNotional:   formatFloat(notional),
		ExpectedHoldingSecs: 1,
		Confidence:          confidence,
		MarketQuality:       quality,
		CalculationVersion:  "1.0.0",
		CreatedAt:           now,
		UpdatedAt:           now,
		Legs: []*OpportunityLeg{
			{
				ID:                   uuid.New(),
				OpportunityID:        oppID,
				Side:                 LegSideBuy,
				VenueID:              input.BuyVenueID,
				VenueCode:            input.BuyVenueCode,
				InstrumentID:         input.InstrumentID,
				TargetSize:           formatFloat(size),
				TargetNotional:       formatFloat(notional),
				ExpectedPrice:        *input.BuyVenue.BestAsk,
				FeeBPS:               buyFee.TakerFeeBPS,
				EstimatedSlippageBPS: slippageBPS,
				Metadata: &LegMeta{
					BestBid:         derefStr(input.BuyVenue.BestBid),
					BestAsk:         derefStr(input.BuyVenue.BestAsk),
					Spread:          derefStr(input.BuyVenue.Spread),
					HealthStatus:    string(input.BuyVenue.Health),
					LastUpdateAgeMs: time.Since(input.BuyVenue.LastUpdate).Milliseconds(),
				},
			},
			{
				ID:                   uuid.New(),
				OpportunityID:        oppID,
				Side:                 LegSideSell,
				VenueID:              input.SellVenueID,
				VenueCode:            input.SellVenueCode,
				InstrumentID:         input.InstrumentID,
				TargetSize:           formatFloat(size),
				TargetNotional:       formatFloat(notional),
				ExpectedPrice:        *input.SellVenue.BestBid,
				FeeBPS:               sellFee.TakerFeeBPS,
				EstimatedSlippageBPS: slippageBPS,
				Metadata: &LegMeta{
					BestBid:         derefStr(input.SellVenue.BestBid),
					BestAsk:         derefStr(input.SellVenue.BestAsk),
					Spread:          derefStr(input.SellVenue.Spread),
					HealthStatus:    string(input.SellVenue.Health),
					LastUpdateAgeMs: time.Since(input.SellVenue.LastUpdate).Milliseconds(),
				},
			},
		},
	}

	return opp, nil
}

func (c *Calculator) CalculateFundingArb(input *FundingArbInput) (*Opportunity, error) {
	if input.VenueA == nil || input.VenueB == nil {
		return nil, ErrVenueNotHealthy
	}

	if input.VenueA.Health != unifiedstate.VenueHealthHealthy ||
		input.VenueB.Health != unifiedstate.VenueHealthHealthy {
		return nil, ErrVenueNotHealthy
	}

	if input.VenueA.Funding == nil || input.VenueB.Funding == nil {
		return nil, ErrNoFundingData
	}

	rateA := parseFloat(&input.VenueA.Funding.FundingRate)
	rateB := parseFloat(&input.VenueB.Funding.FundingRate)

	if rateA == 0 && rateB == 0 {
		return nil, ErrBelowMinEdge
	}

	intervalA := input.VenueA.Funding.IntervalSeconds
	intervalB := input.VenueB.Funding.IntervalSeconds
	if intervalA <= 0 {
		intervalA = 3600
	}
	if intervalB <= 0 {
		intervalB = 3600
	}

	rateAHourly := rateA * 3600.0 / float64(intervalA)
	rateBHourly := rateB * 3600.0 / float64(intervalB)

	netRateHourly := rateAHourly - rateBHourly
	if netRateHourly < 0 {
		netRateHourly = -netRateHourly
	}

	netEdgeBPS := int(netRateHourly * 10000)

	if netEdgeBPS < c.config.MinGrossEdgeBPS {
		return nil, ErrBelowMinEdge
	}

	grossEdge := netRateHourly

	buyFee := c.getFeeModel(input.VenueACode)
	sellFee := c.getFeeModel(input.VenueBCode)
	totalFeeBPS := buyFee.TakerFeeBPS + sellFee.TakerFeeBPS
	feeAmount := grossEdge * float64(totalFeeBPS) / 10000.0

	slippageBPS := c.estimateSlippage(input.VenueA, input.VenueB)
	slippageAmount := grossEdge * float64(slippageBPS) / 10000.0

	netEdge := grossEdge - feeAmount - slippageAmount
	if netEdge <= 0 {
		return nil, ErrBelowMinEdge
	}

	size, notional := c.calculateSizing(1.0, netEdge*10000)

	confidence := c.calculateFundingConfidence(input.VenueA, input.VenueB, netEdgeBPS)

	if confidence < c.config.MinConfidence {
		return nil, ErrBelowMinConfidence
	}

	quality := c.assessMarketQuality(input.VenueA, input.VenueB)

	var longVenueID, shortVenueID uuid.UUID
	var longVenueCode, shortVenueCode string
	if rateAHourly < rateBHourly {
		longVenueID = input.VenueAID
		longVenueCode = input.VenueACode
		shortVenueID = input.VenueBID
		shortVenueCode = input.VenueBCode
	} else {
		longVenueID = input.VenueBID
		longVenueCode = input.VenueBCode
		shortVenueID = input.VenueAID
		shortVenueCode = input.VenueACode
	}

	now := time.Now()
	oppID := uuid.New()

	opp := &Opportunity{
		ID:                  oppID,
		OpportunityType:     OpportunityTypeFundingArb,
		Status:              OpportunityStatusDetected,
		InstrumentID:        input.InstrumentID,
		CanonicalSymbol:     input.CanonicalSymbol,
		DetectedAt:          now,
		ExpiresAt:           now.Add(c.config.OpportunityTTL),
		BuyVenueID:          longVenueID,
		BuyVenueCode:        longVenueCode,
		SellVenueID:         shortVenueID,
		SellVenueCode:       shortVenueCode,
		GrossEdge:           formatFloat(grossEdge),
		GrossEdgeBPS:        netEdgeBPS,
		EstimatedFees:       formatFloat(feeAmount),
		EstimatedSlippage:   formatFloat(slippageAmount),
		EstimatedOtherCosts: "0",
		ExpectedNetEdge:     formatFloat(netEdge),
		ExpectedNetEdgeBPS:  int(netEdge * 10000),
		ExpectedNetPnl:      formatFloat(netEdge * notional),
		SuggestedSize:       formatFloat(size),
		SuggestedNotional:   formatFloat(notional),
		ExpectedHoldingSecs: 3600,
		Confidence:          confidence,
		MarketQuality:       quality,
		CalculationVersion:  "1.0.0",
		CreatedAt:           now,
		UpdatedAt:           now,
		Legs: []*OpportunityLeg{
			{
				ID:                   uuid.New(),
				OpportunityID:        oppID,
				Side:                 LegSideBuy,
				VenueID:              longVenueID,
				VenueCode:            longVenueCode,
				InstrumentID:         input.InstrumentID,
				TargetSize:           formatFloat(size),
				TargetNotional:       formatFloat(notional),
				ExpectedPrice:        derefStr(input.VenueA.BestAsk),
				FeeBPS:               buyFee.TakerFeeBPS,
				EstimatedSlippageBPS: slippageBPS,
			},
			{
				ID:                   uuid.New(),
				OpportunityID:        oppID,
				Side:                 LegSideSell,
				VenueID:              shortVenueID,
				VenueCode:            shortVenueCode,
				InstrumentID:         input.InstrumentID,
				TargetSize:           formatFloat(size),
				TargetNotional:       formatFloat(notional),
				ExpectedPrice:        derefStr(input.VenueB.BestBid),
				FeeBPS:               sellFee.TakerFeeBPS,
				EstimatedSlippageBPS: slippageBPS,
			},
		},
	}

	return opp, nil
}

func (c *Calculator) CalculateBasisArb(input *BasisArbInput) (*Opportunity, error) {
	if input.Venue == nil {
		return nil, ErrVenueNotHealthy
	}

	if input.Venue.Health != unifiedstate.VenueHealthHealthy {
		return nil, ErrVenueNotHealthy
	}

	if input.Venue.MarkPrice == nil || input.Venue.IndexPrice == nil {
		return nil, ErrInsufficientDepth
	}

	markPrice := parseFloat(input.Venue.MarkPrice)
	indexPrice := parseFloat(input.Venue.IndexPrice)

	if markPrice <= 0 || indexPrice <= 0 {
		return nil, ErrInvalidPrice
	}

	basis := markPrice - indexPrice
	basisBPS := int((basis / indexPrice) * 10000)

	absBasisBPS := basisBPS
	if absBasisBPS < 0 {
		absBasisBPS = -absBasisBPS
	}

	if absBasisBPS < c.config.MinGrossEdgeBPS {
		return nil, ErrBelowMinEdge
	}

	fee := c.getFeeModel(input.VenueCode)
	totalFeeBPS := fee.TakerFeeBPS * 2
	feeAmount := indexPrice * float64(totalFeeBPS) / 10000.0

	slippageBPS := c.estimateSlippageSingle(input.Venue)
	slippageAmount := indexPrice * float64(slippageBPS) / 10000.0

	grossEdgeAbs := basis
	if grossEdgeAbs < 0 {
		grossEdgeAbs = -grossEdgeAbs
	}
	netEdge := grossEdgeAbs - feeAmount - slippageAmount
	netEdgeBPS := int((netEdge / indexPrice) * 10000)

	if netEdgeBPS < c.config.MinNetEdgeBPS {
		return nil, ErrBelowMinEdge
	}

	size, notional := c.calculateSizing(indexPrice, netEdge)

	confidence := c.calculateBasisConfidence(input.Venue, netEdgeBPS)

	if confidence < c.config.MinConfidence {
		return nil, ErrBelowMinConfidence
	}

	quality := c.assessMarketQualitySingle(input.Venue)

	now := time.Now()
	oppID := uuid.New()

	opp := &Opportunity{
		ID:                  oppID,
		OpportunityType:     OpportunityTypeBasisArb,
		Status:              OpportunityStatusDetected,
		InstrumentID:        input.InstrumentID,
		CanonicalSymbol:     input.CanonicalSymbol,
		DetectedAt:          now,
		ExpiresAt:           now.Add(c.config.OpportunityTTL),
		BuyVenueID:          input.VenueID,
		BuyVenueCode:        input.VenueCode,
		SellVenueID:         input.VenueID,
		SellVenueCode:       input.VenueCode,
		GrossEdge:           formatFloat(grossEdgeAbs),
		GrossEdgeBPS:        absBasisBPS,
		EstimatedFees:       formatFloat(feeAmount),
		EstimatedSlippage:   formatFloat(slippageAmount),
		EstimatedOtherCosts: "0",
		ExpectedNetEdge:     formatFloat(netEdge),
		ExpectedNetEdgeBPS:  netEdgeBPS,
		ExpectedNetPnl:      formatFloat(netEdge * size),
		SuggestedSize:       formatFloat(size),
		SuggestedNotional:   formatFloat(notional),
		ExpectedHoldingSecs: 3600,
		Confidence:          confidence,
		MarketQuality:       quality,
		CalculationVersion:  "1.0.0",
		CreatedAt:           now,
		UpdatedAt:           now,
		Legs: []*OpportunityLeg{
			{
				ID:                   uuid.New(),
				OpportunityID:        oppID,
				Side:                 LegSideBuy,
				VenueID:              input.VenueID,
				VenueCode:            input.VenueCode,
				InstrumentID:         input.InstrumentID,
				TargetSize:           formatFloat(size),
				TargetNotional:       formatFloat(notional),
				ExpectedPrice:        *input.Venue.MarkPrice,
				FeeBPS:               fee.TakerFeeBPS,
				EstimatedSlippageBPS: slippageBPS,
			},
			{
				ID:                   uuid.New(),
				OpportunityID:        oppID,
				Side:                 LegSideSell,
				VenueID:              input.VenueID,
				VenueCode:            input.VenueCode,
				InstrumentID:         input.InstrumentID,
				TargetSize:           formatFloat(size),
				TargetNotional:       formatFloat(notional),
				ExpectedPrice:        *input.Venue.IndexPrice,
				FeeBPS:               fee.TakerFeeBPS,
				EstimatedSlippageBPS: slippageBPS,
			},
		},
	}

	return opp, nil
}

func (c *Calculator) getFeeModel(venueCode string) FeeModel {
	if fee, ok := c.config.FeeModels[venueCode]; ok {
		return fee
	}
	return FeeModel{MakerFeeBPS: 5, TakerFeeBPS: 10}
}

func (c *Calculator) estimateSlippage(venueA, venueB *unifiedstate.VenueMarketState) int {
	slippageA := c.estimateSlippageSingle(venueA)
	slippageB := c.estimateSlippageSingle(venueB)
	if slippageA > slippageB {
		return slippageA
	}
	return slippageB
}

func (c *Calculator) estimateSlippageSingle(venue *unifiedstate.VenueMarketState) int {
	if venue == nil {
		return c.config.SlippageModel.MaxSlippageBPS
	}

	totalDepth := 0.0
	for _, level := range venue.BidDepth {
		totalDepth += parseFloat(&level.Quantity)
	}
	for _, level := range venue.AskDepth {
		totalDepth += parseFloat(&level.Quantity)
	}

	if totalDepth < c.config.SlippageModel.DepthThreshold {
		return c.config.SlippageModel.MaxSlippageBPS
	}

	depthRatio := c.config.SlippageModel.DepthThreshold / totalDepth
	if depthRatio > 1 {
		depthRatio = 1
	}

	return int(float64(c.config.SlippageModel.MaxSlippageBPS) * depthRatio)
}

func (c *Calculator) calculateSizing(price float64, netEdge float64) (size float64, notional float64) {
	maxNotional := parseFloatStr(c.config.Capital.MaxNotionalPerTrade)
	minTradeSize := parseFloatStr(c.config.Capital.MinTradeSizeUSD)

	if maxNotional <= 0 {
		maxNotional = 100000
	}
	if minTradeSize <= 0 {
		minTradeSize = 100
	}

	if price <= 0 {
		return 0, 0
	}

	size = maxNotional / price
	notional = maxNotional

	if notional < minTradeSize {
		return 0, 0
	}

	return size, notional
}

func (c *Calculator) calculateConfidence(venueA, venueB *unifiedstate.VenueMarketState, grossBPS, netBPS int) float64 {
	score := 0.5

	if venueA.Health == unifiedstate.VenueHealthHealthy && venueB.Health == unifiedstate.VenueHealthHealthy {
		score += 0.2
	}

	ageA := time.Since(venueA.LastUpdate).Milliseconds()
	ageB := time.Since(venueB.LastUpdate).Milliseconds()
	if ageA < 5000 && ageB < 5000 {
		score += 0.15
	} else if ageA < 30000 && ageB < 30000 {
		score += 0.05
	}

	if netBPS > grossBPS/2 {
		score += 0.1
	}

	if len(venueA.BidDepth) >= 3 && len(venueA.AskDepth) >= 3 &&
		len(venueB.BidDepth) >= 3 && len(venueB.AskDepth) >= 3 {
		score += 0.05
	}

	if score > 1.0 {
		score = 1.0
	}
	return score
}

func (c *Calculator) calculateFundingConfidence(venueA, venueB *unifiedstate.VenueMarketState, netBPS int) float64 {
	score := 0.5

	if venueA.Health == unifiedstate.VenueHealthHealthy && venueB.Health == unifiedstate.VenueHealthHealthy {
		score += 0.2
	}

	if venueA.Funding != nil && venueB.Funding != nil {
		ageA := time.Since(venueA.Funding.ReceivedAt).Milliseconds()
		ageB := time.Since(venueB.Funding.ReceivedAt).Milliseconds()
		if ageA < 300000 && ageB < 300000 {
			score += 0.2
		} else if ageA < 900000 && ageB < 900000 {
			score += 0.1
		}
	}

	if netBPS > 50 {
		score += 0.1
	}

	if score > 1.0 {
		score = 1.0
	}
	return score
}

func (c *Calculator) calculateBasisConfidence(venue *unifiedstate.VenueMarketState, netBPS int) float64 {
	score := 0.5

	if venue.Health == unifiedstate.VenueHealthHealthy {
		score += 0.2
	}

	age := time.Since(venue.LastUpdate).Milliseconds()
	if age < 5000 {
		score += 0.15
	} else if age < 30000 {
		score += 0.05
	}

	if venue.MarkPrice != nil && venue.IndexPrice != nil {
		score += 0.1
	}

	if netBPS > 20 {
		score += 0.05
	}

	if score > 1.0 {
		score = 1.0
	}
	return score
}

func (c *Calculator) assessMarketQuality(venueA, venueB *unifiedstate.VenueMarketState) MarketQuality {
	if venueA.Health != unifiedstate.VenueHealthHealthy ||
		venueB.Health != unifiedstate.VenueHealthHealthy {
		return MarketQualityUnusable
	}

	ageA := time.Since(venueA.LastUpdate).Milliseconds()
	ageB := time.Since(venueB.LastUpdate).Milliseconds()

	if ageA > 30000 || ageB > 30000 {
		return MarketQualityDegraded
	}

	if len(venueA.BidDepth) < 2 || len(venueA.AskDepth) < 2 ||
		len(venueB.BidDepth) < 2 || len(venueB.AskDepth) < 2 {
		return MarketQualityDegraded
	}

	return MarketQualityGood
}

func (c *Calculator) assessMarketQualitySingle(venue *unifiedstate.VenueMarketState) MarketQuality {
	if venue.Health != unifiedstate.VenueHealthHealthy {
		return MarketQualityUnusable
	}

	age := time.Since(venue.LastUpdate).Milliseconds()
	if age > 30000 {
		return MarketQualityDegraded
	}

	if len(venue.BidDepth) < 2 || len(venue.AskDepth) < 2 {
		return MarketQualityDegraded
	}

	return MarketQualityGood
}

func parseFloat(s *string) float64 {
	if s == nil {
		return 0
	}
	f := new(big.Float)
	_, ok := f.SetString(*s)
	if !ok {
		return 0
	}
	val, _ := f.Float64()
	return val
}

func parseFloatStr(s string) float64 {
	f := new(big.Float)
	_, ok := f.SetString(s)
	if !ok {
		return 0
	}
	val, _ := f.Float64()
	return val
}

func formatFloat(f float64) string {
	bf := new(big.Float).SetFloat64(f)
	bf.SetPrec(20)
	return bf.Text('f', -1)
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
