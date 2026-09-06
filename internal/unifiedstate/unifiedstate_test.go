package unifiedstate

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestEngine_GetOrCreateVenueState(t *testing.T) {
	engine := NewEngine()
	venueID := uuid.New()
	instrumentID := uuid.New()

	vs := engine.GetOrCreateVenueState(venueID, instrumentID, "binance")

	if vs == nil {
		t.Fatal("expected venue state to be created")
	}
	if vs.VenueID != venueID {
		t.Errorf("expected venue ID %s, got %s", venueID, vs.VenueID)
	}
	if vs.InstrumentID != instrumentID {
		t.Errorf("expected instrument ID %s, got %s", instrumentID, vs.InstrumentID)
	}
	if vs.VenueCode != "binance" {
		t.Errorf("expected venue code binance, got %s", vs.VenueCode)
	}
}

func TestEngine_GetOrCreateVenueState_Idempotent(t *testing.T) {
	engine := NewEngine()
	venueID := uuid.New()
	instrumentID := uuid.New()

	vs1 := engine.GetOrCreateVenueState(venueID, instrumentID, "binance")
	vs2 := engine.GetOrCreateVenueState(venueID, instrumentID, "binance")

	if vs1 != vs2 {
		t.Error("expected same venue state instance")
	}
}

func TestEngine_GetOrCreateInstrumentState(t *testing.T) {
	engine := NewEngine()
	instrumentID := uuid.New()

	is := engine.GetOrCreateInstrumentState(instrumentID, "BTC-USDT", "BTC", "USDT")

	if is == nil {
		t.Fatal("expected instrument state to be created")
	}
	if is.InstrumentID != instrumentID {
		t.Errorf("expected instrument ID %s, got %s", instrumentID, is.InstrumentID)
	}
	if is.CanonicalSymbol != "BTC-USDT" {
		t.Errorf("expected canonical symbol BTC-USDT, got %s", is.CanonicalSymbol)
	}
}

func TestEngine_MergeVenueState_BestBidAsk(t *testing.T) {
	engine := NewEngine()
	venueID1 := uuid.New()
	venueID2 := uuid.New()
	instrumentID := uuid.New()

	engine.GetOrCreateInstrumentState(instrumentID, "BTC-USDT", "BTC", "USDT")

	vs1 := engine.GetOrCreateVenueState(venueID1, instrumentID, "binance")
	vs2 := engine.GetOrCreateVenueState(venueID2, instrumentID, "okx")

	bid1 := "50000"
	ask1 := "50100"
	vs1.UpdateTicker(&bid1, &ask1, &bid1, nil)
	vs1.UpdateHealth(VenueHealthHealthy)

	bid2 := "50050"
	ask2 := "50080"
	vs2.UpdateTicker(&bid2, &ask2, &bid2, nil)
	vs2.UpdateHealth(VenueHealthHealthy)

	engine.MergeVenueState(instrumentID, vs1)
	engine.MergeVenueState(instrumentID, vs2)

	is := engine.GetInstrumentState(instrumentID)
	if is == nil {
		t.Fatal("expected instrument state to exist")
	}

	if is.BestBid == nil || *is.BestBid != "50050" {
		t.Errorf("expected best bid 50050, got %v", is.BestBid)
	}
	if is.BestAsk == nil || *is.BestAsk != "50080" {
		t.Errorf("expected best ask 50080, got %v", is.BestAsk)
	}
}

func TestEngine_MergeVenueState_Funding(t *testing.T) {
	engine := NewEngine()
	venueID1 := uuid.New()
	venueID2 := uuid.New()
	instrumentID := uuid.New()

	engine.GetOrCreateInstrumentState(instrumentID, "BTC-USDT", "BTC", "USDT")

	vs1 := engine.GetOrCreateVenueState(venueID1, instrumentID, "binance")
	vs2 := engine.GetOrCreateVenueState(venueID2, instrumentID, "okx")

	vs1.UpdateHealth(VenueHealthHealthy)
	vs2.UpdateHealth(VenueHealthHealthy)

	now := time.Now()
	funding1 := &FundingState{
		VenueID:      venueID1,
		FundingRate:  "0.0001",
		ReceivedAt:   now.Add(-1 * time.Hour),
	}
	funding2 := &FundingState{
		VenueID:      venueID2,
		FundingRate:  "0.0002",
		ReceivedAt:   now,
	}

	vs1.UpdateFunding(funding1)
	vs2.UpdateFunding(funding2)

	engine.MergeVenueState(instrumentID, vs1)
	engine.MergeVenueState(instrumentID, vs2)

	is := engine.GetInstrumentState(instrumentID)
	if is == nil {
		t.Fatal("expected instrument state to exist")
	}

	if is.Funding == nil {
		t.Fatal("expected funding to exist")
	}
	if is.Funding.FundingRate != "0.0002" {
		t.Errorf("expected funding rate 0.0002, got %s", is.Funding.FundingRate)
	}
	if is.FundingVenue == nil || *is.FundingVenue != venueID2 {
		t.Error("expected funding venue to be venue2")
	}
}

func TestEngine_ExcludeStaleVenues(t *testing.T) {
	engine := NewEngine()
	venueID1 := uuid.New()
	venueID2 := uuid.New()
	instrumentID := uuid.New()

	engine.GetOrCreateInstrumentState(instrumentID, "BTC-USDT", "BTC", "USDT")

	vs1 := engine.GetOrCreateVenueState(venueID1, instrumentID, "binance")
	vs2 := engine.GetOrCreateVenueState(venueID2, instrumentID, "okx")

	vs1.UpdateHealth(VenueHealthHealthy)
	vs2.UpdateHealth(VenueHealthHealthy)

	vs1.LastUpdate = time.Now().Add(-10 * time.Second)
	vs2.LastUpdate = time.Now()

	staleVenues := engine.ExcludeStaleVenues(5 * time.Second)

	if len(staleVenues) != 1 {
		t.Errorf("expected 1 stale venue, got %d", len(staleVenues))
	}
	if len(staleVenues) > 0 && staleVenues[0] != venueID1 {
		t.Error("expected venue1 to be stale")
	}
}

func TestEngine_GetExecutableDepth(t *testing.T) {
	engine := NewEngine()
	venueID := uuid.New()
	instrumentID := uuid.New()

	engine.GetOrCreateInstrumentState(instrumentID, "BTC-USDT", "BTC", "USDT")

	vs := engine.GetOrCreateVenueState(venueID, instrumentID, "binance")
	vs.UpdateHealth(VenueHealthHealthy)

	bids := []PriceLevel{
		{Price: "50000", Quantity: "10"},
		{Price: "49999", Quantity: "20"},
	}
	asks := []PriceLevel{
		{Price: "50100", Quantity: "5"},
		{Price: "50101", Quantity: "15"},
	}
	bestBid := "50000"
	bestAsk := "50100"
	vs.UpdateOrderBook(bids, asks, &bestBid, &bestAsk)

	engine.MergeVenueState(instrumentID, vs)

	depth := engine.GetExecutableDepth(instrumentID, 2)
	if depth == nil {
		t.Fatal("expected depth to be returned")
	}
	if depth.BestBid == nil || *depth.BestBid != "50000" {
		t.Errorf("expected best bid 50000, got %v", depth.BestBid)
	}
	if depth.BestAsk == nil || *depth.BestAsk != "50100" {
		t.Errorf("expected best ask 50100, got %v", depth.BestAsk)
	}
	if len(depth.BidDepth) != 2 {
		t.Errorf("expected 2 bid depth levels, got %d", len(depth.BidDepth))
	}
	if len(depth.AskDepth) != 2 {
		t.Errorf("expected 2 ask depth levels, got %d", len(depth.AskDepth))
	}
}

func TestEngine_GetHealthOverview(t *testing.T) {
	engine := NewEngine()
	venueID1 := uuid.New()
	venueID2 := uuid.New()
	instrumentID := uuid.New()

	engine.GetOrCreateInstrumentState(instrumentID, "BTC-USDT", "BTC", "USDT")

	vs1 := engine.GetOrCreateVenueState(venueID1, instrumentID, "binance")
	vs2 := engine.GetOrCreateVenueState(venueID2, instrumentID, "okx")

	vs1.UpdateHealth(VenueHealthHealthy)
	vs2.UpdateHealth(VenueHealthStale)

	engine.MergeVenueState(instrumentID, vs1)
	engine.MergeVenueState(instrumentID, vs2)

	overview := engine.GetHealthOverview()

	if overview == nil {
		t.Fatal("expected health overview to be returned")
	}
	if overview.TotalInstruments != 1 {
		t.Errorf("expected 1 instrument, got %d", overview.TotalInstruments)
	}
	if overview.HealthyVenues != 1 {
		t.Errorf("expected 1 healthy venue, got %d", overview.HealthyVenues)
	}
	if overview.StaleVenues != 1 {
		t.Errorf("expected 1 stale venue, got %d", overview.StaleVenues)
	}
}

func TestEngine_GetSnapshot(t *testing.T) {
	engine := NewEngine()
	instrumentID1 := uuid.New()
	instrumentID2 := uuid.New()

	engine.GetOrCreateInstrumentState(instrumentID1, "BTC-USDT", "BTC", "USDT")
	engine.GetOrCreateInstrumentState(instrumentID2, "ETH-USDT", "ETH", "USDT")

	snapshot := engine.GetSnapshot()

	if snapshot == nil {
		t.Fatal("expected snapshot to be returned")
	}
	if snapshot.TotalInstruments != 2 {
		t.Errorf("expected 2 instruments, got %d", snapshot.TotalInstruments)
	}
}

func TestEngine_RecalculateHealth(t *testing.T) {
	engine := NewEngine()
	venueID1 := uuid.New()
	venueID2 := uuid.New()
	instrumentID := uuid.New()

	engine.GetOrCreateInstrumentState(instrumentID, "BTC-USDT", "BTC", "USDT")

	vs1 := engine.GetOrCreateVenueState(venueID1, instrumentID, "binance")
	vs2 := engine.GetOrCreateVenueState(venueID2, instrumentID, "okx")

	vs1.UpdateHealth(VenueHealthHealthy)
	vs2.UpdateHealth(VenueHealthUnhealthy)

	engine.MergeVenueState(instrumentID, vs1)
	engine.MergeVenueState(instrumentID, vs2)

	is := engine.GetInstrumentState(instrumentID)
	if is.HealthyVenues != 1 {
		t.Errorf("expected 1 healthy venue, got %d", is.HealthyVenues)
	}
	if is.TotalVenues != 2 {
		t.Errorf("expected 2 total venues, got %d", is.TotalVenues)
	}
}

func TestEngine_RecalculateDepth_Merge(t *testing.T) {
	engine := NewEngine()
	venueID1 := uuid.New()
	venueID2 := uuid.New()
	instrumentID := uuid.New()

	engine.GetOrCreateInstrumentState(instrumentID, "BTC-USDT", "BTC", "USDT")

	vs1 := engine.GetOrCreateVenueState(venueID1, instrumentID, "binance")
	vs2 := engine.GetOrCreateVenueState(venueID2, instrumentID, "okx")

	vs1.UpdateHealth(VenueHealthHealthy)
	vs2.UpdateHealth(VenueHealthHealthy)

	bids1 := []PriceLevel{{Price: "50000", Quantity: "10"}}
	asks1 := []PriceLevel{{Price: "50100", Quantity: "5"}}
	bestBid1 := "50000"
	bestAsk1 := "50100"
	vs1.UpdateOrderBook(bids1, asks1, &bestBid1, &bestAsk1)

	bids2 := []PriceLevel{{Price: "50001", Quantity: "15"}}
	asks2 := []PriceLevel{{Price: "50099", Quantity: "8"}}
	bestBid2 := "50001"
	bestAsk2 := "50099"
	vs2.UpdateOrderBook(bids2, asks2, &bestBid2, &bestAsk2)

	engine.MergeVenueState(instrumentID, vs1)
	engine.MergeVenueState(instrumentID, vs2)

	is := engine.GetInstrumentState(instrumentID)
	if len(is.BidDepth) != 2 {
		t.Errorf("expected 2 merged bid depth levels, got %d", len(is.BidDepth))
	}
	if len(is.AskDepth) != 2 {
		t.Errorf("expected 2 merged ask depth levels, got %d", len(is.AskDepth))
	}
}

func TestPriceGreater(t *testing.T) {
	if !priceGreater("50100", "50000") {
		t.Error("expected 50100 > 50000")
	}
	if priceGreater("50000", "50100") {
		t.Error("expected 50000 < 50100")
	}
	if priceGreater("50000", "50000") {
		t.Error("expected 50000 == 50000")
	}
}

func TestPriceLess(t *testing.T) {
	if !priceLess("50000", "50100") {
		t.Error("expected 50000 < 50100")
	}
	if priceLess("50100", "50000") {
		t.Error("expected 50100 > 50000")
	}
}

func TestCalculateSpread(t *testing.T) {
	spread := calculateSpread("50000", "50100")
	if spread != "100" {
		t.Errorf("expected spread 100, got %s", spread)
	}
}

func TestCalculateNotional(t *testing.T) {
	notional := calculateNotional("50000", "10")
	if notional != "500000" {
		t.Errorf("expected notional 500000, got %s", notional)
	}
}

func TestTrimTrailingZeros(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"100.00", "100"},
		{"100.50", "100.5"},
		{"100.0", "100"},
		{"100.", "100"},
		{"0", "0"},
		{"", "0"},
	}

	for _, tt := range tests {
		result := trimTrailingZeros(tt.input)
		if result != tt.expected {
			t.Errorf("trimTrailingZeros(%s) = %s, expected %s", tt.input, result, tt.expected)
		}
	}
}

func TestVenueMarketState_UpdateTicker(t *testing.T) {
	vs := &VenueMarketState{
		VenueID:      uuid.New(),
		InstrumentID: uuid.New(),
	}

	bid := "50000"
	ask := "50100"
	last := "50050"
	spread := "100"

	vs.UpdateTicker(&bid, &ask, &last, &spread)

	if vs.BestBid == nil || *vs.BestBid != "50000" {
		t.Errorf("expected best bid 50000, got %v", vs.BestBid)
	}
	if vs.BestAsk == nil || *vs.BestAsk != "50100" {
		t.Errorf("expected best ask 50100, got %v", vs.BestAsk)
	}
}

func TestVenueMarketState_GetSnapshot(t *testing.T) {
	vs := &VenueMarketState{
		VenueID:      uuid.New(),
		InstrumentID: uuid.New(),
		VenueCode:    "binance",
		Health:       VenueHealthHealthy,
	}

	bid := "50000"
	vs.BestBid = &bid

	snap := vs.GetSnapshot()

	if snap.VenueID != vs.VenueID {
		t.Error("expected same venue ID")
	}
	if snap.VenueCode != "binance" {
		t.Error("expected same venue code")
	}
}
