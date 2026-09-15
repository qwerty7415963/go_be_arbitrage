package opportunity

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/unifiedstate"
)

func strPtr(s string) *string {
	return &s
}

func timePtr(t time.Time) *time.Time {
	return &t
}

func TestFeeModel_Percentages(t *testing.T) {
	fee := FeeModel{MakerFeeBPS: 2, TakerFeeBPS: 4}
	if fee.MakerFeePercent() != 0.0002 {
		t.Errorf("expected maker fee 0.0002, got %f", fee.MakerFeePercent())
	}
	if fee.TakerFeePercent() != 0.0004 {
		t.Errorf("expected taker fee 0.0004, got %f", fee.TakerFeePercent())
	}
}

func TestSlippageModel_Percentage(t *testing.T) {
	slippage := SlippageModel{MaxSlippageBPS: 5}
	if slippage.MaxSlippagePercent() != 0.0005 {
		t.Errorf("expected max slippage 0.0005, got %f", slippage.MaxSlippagePercent())
	}
}

func TestCalculator_PriceArb_Profitable(t *testing.T) {
	config := DefaultScannerConfig()
	config.MinGrossEdgeBPS = 10
	config.MinNetEdgeBPS = 5
	config.MinConfidence = 0.0
	calc := NewCalculator(config)

	bid := strPtr("50000")
	ask := strPtr("50050")
	buyVenue := &unifiedstate.VenueMarketState{
		Health:    unifiedstate.VenueHealthHealthy,
		BestBid:   bid,
		BestAsk:   ask,
		LastUpdate: time.Now(),
		BidDepth:  []unifiedstate.PriceLevel{{Price: "50000", Quantity: "10"}},
		AskDepth:  []unifiedstate.PriceLevel{{Price: "50050", Quantity: "10"}},
	}
	sellVenue := &unifiedstate.VenueMarketState{
		Health:    unifiedstate.VenueHealthHealthy,
		BestBid:   strPtr("50500"),
		BestAsk:   strPtr("50550"),
		LastUpdate: time.Now(),
		BidDepth:  []unifiedstate.PriceLevel{{Price: "50500", Quantity: "10"}},
		AskDepth:  []unifiedstate.PriceLevel{{Price: "50550", Quantity: "10"}},
	}

	input := &PriceArbInput{
		InstrumentID:    uuid.New(),
		CanonicalSymbol: "BTCUSD",
		BuyVenue:        buyVenue,
		SellVenue:       sellVenue,
		BuyVenueID:      uuid.New(),
		BuyVenueCode:    "binance",
		SellVenueID:     uuid.New(),
		SellVenueCode:   "extended",
	}

	opp, err := calc.CalculatePriceArb(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if opp == nil {
		t.Fatal("expected opportunity")
	}

	if opp.OpportunityType != OpportunityTypePriceArb {
		t.Errorf("expected PRICE_ARBITRAGE, got %s", opp.OpportunityType)
	}

	if opp.GrossEdgeBPS <= 0 {
		t.Errorf("expected positive gross edge, got %d", opp.GrossEdgeBPS)
	}

	if opp.ExpectedNetEdgeBPS <= 0 {
		t.Errorf("expected positive net edge, got %d", opp.ExpectedNetEdgeBPS)
	}

	if opp.Confidence <= 0 || opp.Confidence > 1.0 {
		t.Errorf("expected confidence 0-1, got %f", opp.Confidence)
	}

	if len(opp.Legs) != 2 {
		t.Fatalf("expected 2 legs, got %d", len(opp.Legs))
	}

	if opp.Legs[0].Side != LegSideBuy {
		t.Errorf("expected first leg BUY, got %s", opp.Legs[0].Side)
	}
	if opp.Legs[1].Side != LegSideSell {
		t.Errorf("expected second leg SELL, got %s", opp.Legs[1].Side)
	}
}

func TestCalculator_PriceArb_NoEdge(t *testing.T) {
	config := DefaultScannerConfig()
	config.MinGrossEdgeBPS = 10
	calc := NewCalculator(config)

	buyVenue := &unifiedstate.VenueMarketState{
		Health:    unifiedstate.VenueHealthHealthy,
		BestBid:   strPtr("50000"),
		BestAsk:   strPtr("50010"),
		LastUpdate: time.Now(),
		BidDepth:  []unifiedstate.PriceLevel{{Price: "50000", Quantity: "10"}},
		AskDepth:  []unifiedstate.PriceLevel{{Price: "50010", Quantity: "10"}},
	}
	sellVenue := &unifiedstate.VenueMarketState{
		Health:    unifiedstate.VenueHealthHealthy,
		BestBid:   strPtr("50005"),
		BestAsk:   strPtr("50015"),
		LastUpdate: time.Now(),
		BidDepth:  []unifiedstate.PriceLevel{{Price: "50005", Quantity: "10"}},
		AskDepth:  []unifiedstate.PriceLevel{{Price: "50015", Quantity: "10"}},
	}

	input := &PriceArbInput{
		InstrumentID:    uuid.New(),
		CanonicalSymbol: "BTCUSD",
		BuyVenue:        buyVenue,
		SellVenue:       sellVenue,
		BuyVenueID:      uuid.New(),
		BuyVenueCode:    "binance",
		SellVenueID:     uuid.New(),
		SellVenueCode:   "extended",
	}

	opp, err := calc.CalculatePriceArb(input)
	if err != ErrBelowMinEdge {
		t.Errorf("expected ErrBelowMinEdge, got %v", err)
	}
	if opp != nil {
		t.Error("expected nil opportunity")
	}
}

func TestCalculator_PriceArb_UnhealthyVenue(t *testing.T) {
	calc := NewCalculator(DefaultScannerConfig())

	buyVenue := &unifiedstate.VenueMarketState{
		Health:    unifiedstate.VenueHealthStale,
		BestBid:   strPtr("50000"),
		BestAsk:   strPtr("50010"),
		LastUpdate: time.Now(),
	}
	sellVenue := &unifiedstate.VenueMarketState{
		Health:    unifiedstate.VenueHealthHealthy,
		BestBid:   strPtr("50100"),
		BestAsk:   strPtr("50150"),
		LastUpdate: time.Now(),
	}

	input := &PriceArbInput{
		InstrumentID:    uuid.New(),
		CanonicalSymbol: "BTCUSD",
		BuyVenue:        buyVenue,
		SellVenue:       sellVenue,
		BuyVenueID:      uuid.New(),
		BuyVenueCode:    "binance",
		SellVenueID:     uuid.New(),
		SellVenueCode:   "extended",
	}

	opp, err := calc.CalculatePriceArb(input)
	if err != ErrVenueNotHealthy {
		t.Errorf("expected ErrVenueNotHealthy, got %v", err)
	}
	if opp != nil {
		t.Error("expected nil opportunity")
	}
}

func TestCalculator_PriceArb_NilVenue(t *testing.T) {
	calc := NewCalculator(DefaultScannerConfig())

	input := &PriceArbInput{
		InstrumentID:    uuid.New(),
		CanonicalSymbol: "BTCUSD",
		BuyVenue:        nil,
		SellVenue:       nil,
		BuyVenueID:      uuid.New(),
		BuyVenueCode:    "binance",
		SellVenueID:     uuid.New(),
		SellVenueCode:   "extended",
	}

	opp, err := calc.CalculatePriceArb(input)
	if err != ErrVenueNotHealthy {
		t.Errorf("expected ErrVenueNotHealthy, got %v", err)
	}
	if opp != nil {
		t.Error("expected nil opportunity")
	}
}

func TestCalculator_FundingArb_Profitable(t *testing.T) {
	config := DefaultScannerConfig()
	config.MinGrossEdgeBPS = 10
	config.MinNetEdgeBPS = 5
	config.MinConfidence = 0.0
	calc := NewCalculator(config)

	venueA := &unifiedstate.VenueMarketState{
		Health:    unifiedstate.VenueHealthHealthy,
		BestBid:   strPtr("50000"),
		BestAsk:   strPtr("50010"),
		LastUpdate: time.Now(),
		Funding: &unifiedstate.FundingState{
			FundingRate:     "0.0001",
			IntervalSeconds: 3600,
			ReceivedAt:      time.Now(),
		},
	}
	venueB := &unifiedstate.VenueMarketState{
		Health:    unifiedstate.VenueHealthHealthy,
		BestBid:   strPtr("50020"),
		BestAsk:   strPtr("50030"),
		LastUpdate: time.Now(),
		Funding: &unifiedstate.FundingState{
			FundingRate:     "0.002",
			IntervalSeconds: 3600,
			ReceivedAt:      time.Now(),
		},
	}

	input := &FundingArbInput{
		InstrumentID:    uuid.New(),
		CanonicalSymbol: "BTCUSD",
		VenueA:          venueA,
		VenueB:          venueB,
		VenueAID:        uuid.New(),
		VenueACode:      "binance",
		VenueBID:        uuid.New(),
		VenueBCode:      "extended",
	}

	opp, err := calc.CalculateFundingArb(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if opp == nil {
		t.Fatal("expected opportunity")
	}

	if opp.OpportunityType != OpportunityTypeFundingArb {
		t.Errorf("expected FUNDING_ARBITRAGE, got %s", opp.OpportunityType)
	}

	if opp.GrossEdgeBPS <= 0 {
		t.Errorf("expected positive gross edge, got %d", opp.GrossEdgeBPS)
	}

	if len(opp.Legs) != 2 {
		t.Fatalf("expected 2 legs, got %d", len(opp.Legs))
	}
}

func TestCalculator_FundingArb_NoData(t *testing.T) {
	calc := NewCalculator(DefaultScannerConfig())

	venueA := &unifiedstate.VenueMarketState{
		Health:     unifiedstate.VenueHealthHealthy,
		BestBid:    strPtr("50000"),
		BestAsk:    strPtr("50010"),
		LastUpdate: time.Now(),
		Funding:    nil,
	}
	venueB := &unifiedstate.VenueMarketState{
		Health:     unifiedstate.VenueHealthHealthy,
		BestBid:    strPtr("50020"),
		BestAsk:    strPtr("50030"),
		LastUpdate: time.Now(),
		Funding:    nil,
	}

	input := &FundingArbInput{
		InstrumentID:    uuid.New(),
		CanonicalSymbol: "BTCUSD",
		VenueA:          venueA,
		VenueB:          venueB,
		VenueAID:        uuid.New(),
		VenueACode:      "binance",
		VenueBID:        uuid.New(),
		VenueBCode:      "extended",
	}

	opp, err := calc.CalculateFundingArb(input)
	if err != ErrNoFundingData {
		t.Errorf("expected ErrNoFundingData, got %v", err)
	}
	if opp != nil {
		t.Error("expected nil opportunity")
	}
}

func TestCalculator_BasisArb_Profitable(t *testing.T) {
	config := DefaultScannerConfig()
	config.MinGrossEdgeBPS = 10
	config.MinNetEdgeBPS = 5
	config.MinConfidence = 0.0
	calc := NewCalculator(config)

	venue := &unifiedstate.VenueMarketState{
		Health:     unifiedstate.VenueHealthHealthy,
		BestBid:    strPtr("50000"),
		BestAsk:    strPtr("50010"),
		MarkPrice:  strPtr("50500"),
		IndexPrice: strPtr("50000"),
		LastUpdate: time.Now(),
		BidDepth:   []unifiedstate.PriceLevel{{Price: "50000", Quantity: "10"}},
		AskDepth:   []unifiedstate.PriceLevel{{Price: "50010", Quantity: "10"}},
	}

	input := &BasisArbInput{
		InstrumentID:    uuid.New(),
		CanonicalSymbol: "BTCUSD",
		Venue:           venue,
		VenueID:         uuid.New(),
		VenueCode:       "binance",
	}

	opp, err := calc.CalculateBasisArb(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if opp == nil {
		t.Fatal("expected opportunity")
	}

	if opp.OpportunityType != OpportunityTypeBasisArb {
		t.Errorf("expected BASIS_ARBITRAGE, got %s", opp.OpportunityType)
	}

	if opp.GrossEdgeBPS <= 0 {
		t.Errorf("expected positive gross edge, got %d", opp.GrossEdgeBPS)
	}

	if len(opp.Legs) != 2 {
		t.Fatalf("expected 2 legs, got %d", len(opp.Legs))
	}
}

func TestCalculator_BasisArb_NoBasis(t *testing.T) {
	calc := NewCalculator(DefaultScannerConfig())

	venue := &unifiedstate.VenueMarketState{
		Health:     unifiedstate.VenueHealthHealthy,
		BestBid:    strPtr("50000"),
		BestAsk:    strPtr("50010"),
		MarkPrice:  strPtr("50000"),
		IndexPrice: strPtr("50000"),
		LastUpdate: time.Now(),
	}

	input := &BasisArbInput{
		InstrumentID:    uuid.New(),
		CanonicalSymbol: "BTCUSD",
		Venue:           venue,
		VenueID:         uuid.New(),
		VenueCode:       "binance",
	}

	opp, err := calc.CalculateBasisArb(input)
	if err != ErrBelowMinEdge {
		t.Errorf("expected ErrBelowMinEdge, got %v", err)
	}
	if opp != nil {
		t.Error("expected nil opportunity")
	}
}

func TestCalculator_Sizing(t *testing.T) {
	calc := NewCalculator(DefaultScannerConfig())

	size, notional := calc.calculateSizing(50000, 100)
	if size != 2 {
		t.Errorf("expected size 2, got %f", size)
	}
	if notional != 100000 {
		t.Errorf("expected notional 100000, got %f", notional)
	}
}

func TestCalculator_Sizing_ZeroPrice(t *testing.T) {
	calc := NewCalculator(DefaultScannerConfig())

	size, notional := calc.calculateSizing(0, 100)
	if size != 0 {
		t.Errorf("expected size 0, got %f", size)
	}
	if notional != 0 {
		t.Errorf("expected notional 0, got %f", notional)
	}
}

func TestCalculator_Confidence_Healthy(t *testing.T) {
	calc := NewCalculator(DefaultScannerConfig())

	venueA := &unifiedstate.VenueMarketState{
		Health:     unifiedstate.VenueHealthHealthy,
		LastUpdate: time.Now(),
		BidDepth:   []unifiedstate.PriceLevel{{Price: "100", Quantity: "10"}, {Price: "99", Quantity: "20"}, {Price: "98", Quantity: "30"}},
		AskDepth:   []unifiedstate.PriceLevel{{Price: "101", Quantity: "10"}, {Price: "102", Quantity: "20"}, {Price: "103", Quantity: "30"}},
	}
	venueB := &unifiedstate.VenueMarketState{
		Health:     unifiedstate.VenueHealthHealthy,
		LastUpdate: time.Now(),
		BidDepth:   []unifiedstate.PriceLevel{{Price: "100", Quantity: "10"}, {Price: "99", Quantity: "20"}, {Price: "98", Quantity: "30"}},
		AskDepth:   []unifiedstate.PriceLevel{{Price: "101", Quantity: "10"}, {Price: "102", Quantity: "20"}, {Price: "103", Quantity: "30"}},
	}

	conf := calc.calculateConfidence(venueA, venueB, 50, 30)
	if conf < 0.8 {
		t.Errorf("expected confidence >= 0.8 for healthy venues, got %f", conf)
	}
}

func TestCalculator_Confidence_Stale(t *testing.T) {
	calc := NewCalculator(DefaultScannerConfig())

	venueA := &unifiedstate.VenueMarketState{
		Health:     unifiedstate.VenueHealthStale,
		LastUpdate: time.Now().Add(-60 * time.Second),
	}
	venueB := &unifiedstate.VenueMarketState{
		Health:     unifiedstate.VenueHealthStale,
		LastUpdate: time.Now().Add(-60 * time.Second),
	}

	conf := calc.calculateConfidence(venueA, venueB, 50, 30)
	if conf > 0.6 {
		t.Errorf("expected lower confidence for stale venues, got %f", conf)
	}
}

func TestCalculator_MarketQuality_Good(t *testing.T) {
	calc := NewCalculator(DefaultScannerConfig())

	venueA := &unifiedstate.VenueMarketState{
		Health:     unifiedstate.VenueHealthHealthy,
		LastUpdate: time.Now(),
		BidDepth:   []unifiedstate.PriceLevel{{Price: "100", Quantity: "10"}, {Price: "99", Quantity: "20"}},
		AskDepth:   []unifiedstate.PriceLevel{{Price: "101", Quantity: "10"}, {Price: "102", Quantity: "20"}},
	}
	venueB := &unifiedstate.VenueMarketState{
		Health:     unifiedstate.VenueHealthHealthy,
		LastUpdate: time.Now(),
		BidDepth:   []unifiedstate.PriceLevel{{Price: "100", Quantity: "10"}, {Price: "99", Quantity: "20"}},
		AskDepth:   []unifiedstate.PriceLevel{{Price: "101", Quantity: "10"}, {Price: "102", Quantity: "20"}},
	}

	quality := calc.assessMarketQuality(venueA, venueB)
	if quality != MarketQualityGood {
		t.Errorf("expected GOOD, got %s", quality)
	}
}

func TestCalculator_MarketQuality_Degraded(t *testing.T) {
	calc := NewCalculator(DefaultScannerConfig())

	venueA := &unifiedstate.VenueMarketState{
		Health:     unifiedstate.VenueHealthHealthy,
		LastUpdate: time.Now().Add(-60 * time.Second),
		BidDepth:   []unifiedstate.PriceLevel{{Price: "100", Quantity: "10"}, {Price: "99", Quantity: "20"}},
		AskDepth:   []unifiedstate.PriceLevel{{Price: "101", Quantity: "10"}, {Price: "102", Quantity: "20"}},
	}
	venueB := &unifiedstate.VenueMarketState{
		Health:     unifiedstate.VenueHealthHealthy,
		LastUpdate: time.Now(),
		BidDepth:   []unifiedstate.PriceLevel{{Price: "100", Quantity: "10"}, {Price: "99", Quantity: "20"}},
		AskDepth:   []unifiedstate.PriceLevel{{Price: "101", Quantity: "10"}, {Price: "102", Quantity: "20"}},
	}

	quality := calc.assessMarketQuality(venueA, venueB)
	if quality != MarketQualityDegraded {
		t.Errorf("expected DEGRADED, got %s", quality)
	}
}

func TestCalculator_MarketQuality_Unusable(t *testing.T) {
	calc := NewCalculator(DefaultScannerConfig())

	venueA := &unifiedstate.VenueMarketState{
		Health:     unifiedstate.VenueHealthUnhealthy,
		LastUpdate: time.Now(),
	}
	venueB := &unifiedstate.VenueMarketState{
		Health:     unifiedstate.VenueHealthHealthy,
		LastUpdate: time.Now(),
	}

	quality := calc.assessMarketQuality(venueA, venueB)
	if quality != MarketQualityUnusable {
		t.Errorf("expected UNUSABLE, got %s", quality)
	}
}

func TestEngine_ScanAll_Empty(t *testing.T) {
	engine := NewEngine(DefaultScannerConfig())

	snapshot := &unifiedstate.UnifiedStateSnapshot{
		Instruments:     make(map[uuid.UUID]*unifiedstate.InstrumentState),
		TotalInstruments: 0,
		Timestamp:       time.Now(),
	}

	result := engine.ScanAll(snapshot)
	if result == nil {
		t.Fatal("expected result")
	}
	if len(result.Opportunities) != 0 {
		t.Errorf("expected 0 opportunities, got %d", len(result.Opportunities))
	}
}

func TestEngine_ScanAll_WithOpportunity(t *testing.T) {
	config := DefaultScannerConfig()
	config.MinGrossEdgeBPS = 10
	config.MinNetEdgeBPS = 5
	config.MinConfidence = 0.0
	engine := NewEngine(config)

	venueAID := uuid.New()
	venueBID := uuid.New()
	instrumentID := uuid.New()

 inst := &unifiedstate.InstrumentState{
		InstrumentID:    instrumentID,
		CanonicalSymbol: "BTCUSD",
		BaseAsset:       "BTC",
		QuoteAsset:      "USD",
		HealthyVenues:   2,
		TotalVenues:     2,
		VenueStates: map[uuid.UUID]*unifiedstate.VenueMarketState{
			venueAID: {
				VenueID:      venueAID,
				VenueCode:    "binance",
				Health:       unifiedstate.VenueHealthHealthy,
				BestBid:      strPtr("50000"),
				BestAsk:      strPtr("50050"),
				LastUpdate:   time.Now(),
				BidDepth:     []unifiedstate.PriceLevel{{Price: "50000", Quantity: "10"}},
				AskDepth:     []unifiedstate.PriceLevel{{Price: "50050", Quantity: "10"}},
			},
			venueBID: {
				VenueID:      venueBID,
				VenueCode:    "extended",
				Health:       unifiedstate.VenueHealthHealthy,
				BestBid:      strPtr("50500"),
				BestAsk:      strPtr("50550"),
				LastUpdate:   time.Now(),
				BidDepth:     []unifiedstate.PriceLevel{{Price: "50500", Quantity: "10"}},
				AskDepth:     []unifiedstate.PriceLevel{{Price: "50550", Quantity: "10"}},
			},
		},
	}

	snapshot := &unifiedstate.UnifiedStateSnapshot{
		Instruments: map[uuid.UUID]*unifiedstate.InstrumentState{
			instrumentID: inst,
		},
		TotalInstruments: 1,
		Timestamp:        time.Now(),
	}

	result := engine.ScanAll(snapshot)
	if result == nil {
		t.Fatal("expected result")
	}

	if len(result.Opportunities) == 0 {
		t.Error("expected at least 1 opportunity")
	}

	for _, opp := range result.Opportunities {
		if opp.OpportunityType != OpportunityTypePriceArb {
			t.Errorf("expected PRICE_ARBITRAGE, got %s", opp.OpportunityType)
		}
		if len(opp.Legs) != 2 {
			t.Errorf("expected 2 legs, got %d", len(opp.Legs))
		}
	}
}

func TestEngine_GetOpportunity(t *testing.T) {
	engine := NewEngine(DefaultScannerConfig())

	opp := &Opportunity{
		ID:            uuid.New(),
		CanonicalSymbol: "BTCUSD",
	}
	engine.results[opp.ID] = opp

	got := engine.GetOpportunity(opp.ID)
	if got == nil {
		t.Fatal("expected opportunity")
	}
	if got.ID != opp.ID {
		t.Errorf("expected ID %s, got %s", opp.ID, got.ID)
	}
}

func TestEngine_RemoveOpportunity(t *testing.T) {
	engine := NewEngine(DefaultScannerConfig())

	opp := &Opportunity{
		ID:            uuid.New(),
		CanonicalSymbol: "BTCUSD",
	}
	engine.results[opp.ID] = opp

	engine.RemoveOpportunity(opp.ID)

	got := engine.GetOpportunity(opp.ID)
	if got != nil {
		t.Error("expected nil after removal")
	}
}

func TestEngine_GetOpportunitiesByType(t *testing.T) {
	engine := NewEngine(DefaultScannerConfig())

	engine.results[uuid.New()] = &Opportunity{ID: uuid.New(), OpportunityType: OpportunityTypePriceArb}
	engine.results[uuid.New()] = &Opportunity{ID: uuid.New(), OpportunityType: OpportunityTypeFundingArb}
	engine.results[uuid.New()] = &Opportunity{ID: uuid.New(), OpportunityType: OpportunityTypePriceArb}

	priceOpps := engine.GetOpportunitiesByType(OpportunityTypePriceArb)
	if len(priceOpps) != 2 {
		t.Errorf("expected 2 price arb opps, got %d", len(priceOpps))
	}

	fundingOpps := engine.GetOpportunitiesByType(OpportunityTypeFundingArb)
	if len(fundingOpps) != 1 {
		t.Errorf("expected 1 funding arb opp, got %d", len(fundingOpps))
	}
}

func TestEngine_ExpireOldOpportunities(t *testing.T) {
	engine := NewEngine(DefaultScannerConfig())

	oldID := uuid.New()
	engine.results[oldID] = &Opportunity{
		ID:        oldID,
		ExpiresAt: time.Now().Add(-1 * time.Second),
	}

	newID := uuid.New()
	engine.results[newID] = &Opportunity{
		ID:        newID,
		ExpiresAt: time.Now().Add(1 * time.Minute),
	}

	engine.expireOldOpportunities()

	if engine.GetOpportunity(oldID) != nil {
		t.Error("expected old opportunity to be expired")
	}
	if engine.GetOpportunity(newID) == nil {
		t.Error("expected new opportunity to remain")
	}
}

func TestDefaultScannerConfig(t *testing.T) {
	config := DefaultScannerConfig()

	if config.MinGrossEdgeBPS != 10 {
		t.Errorf("expected min gross edge 10, got %d", config.MinGrossEdgeBPS)
	}
	if config.MinNetEdgeBPS != 5 {
		t.Errorf("expected min net edge 5, got %d", config.MinNetEdgeBPS)
	}
	if config.MinConfidence != 0.5 {
		t.Errorf("expected min confidence 0.5, got %f", config.MinConfidence)
	}
	if config.OpportunityTTL != 30*time.Second {
		t.Errorf("expected TTL 30s, got %v", config.OpportunityTTL)
	}
	if len(config.FeeModels) != 3 {
		t.Errorf("expected 3 fee models, got %d", len(config.FeeModels))
	}
}

func TestFormatFloat(t *testing.T) {
	result := formatFloat(123.456)
	if result != "123.456" {
		t.Errorf("expected 123.456, got %s", result)
	}

	result = formatFloat(0)
	if result != "0" {
		t.Errorf("expected 0, got %s", result)
	}
}

func TestParseFloat(t *testing.T) {
	s := strPtr("123.456")
	result := parseFloat(s)
	if result != 123.456 {
		t.Errorf("expected 123.456, got %f", result)
	}

	result = parseFloat(nil)
	if result != 0 {
		t.Errorf("expected 0 for nil, got %f", result)
	}

	result = parseFloat(strPtr("invalid"))
	if result != 0 {
		t.Errorf("expected 0 for invalid, got %f", result)
	}
}
