package orderbook

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestEngine_CreateBook(t *testing.T) {
	engine := NewEngine()
	venueID := uuid.New()
	instrumentID := uuid.New()

	book := engine.GetOrCreateBook(venueID, instrumentID, 5*time.Second, 10*time.Second)

	if book == nil {
		t.Fatal("expected book to be created")
	}
	if book.VenueID != venueID {
		t.Errorf("expected venue ID %s, got %s", venueID, book.VenueID)
	}
	if book.InstrumentID != instrumentID {
		t.Errorf("expected instrument ID %s, got %s", instrumentID, book.InstrumentID)
	}
	if book.State != BookStateDisconnected {
		t.Errorf("expected state DISCONNECTED, got %s", book.State)
	}
}

func TestEngine_GetOrCreateBook_Idempotent(t *testing.T) {
	engine := NewEngine()
	venueID := uuid.New()
	instrumentID := uuid.New()

	book1 := engine.GetOrCreateBook(venueID, instrumentID, 5*time.Second, 10*time.Second)
	book2 := engine.GetOrCreateBook(venueID, instrumentID, 5*time.Second, 10*time.Second)

	if book1 != book2 {
		t.Error("expected same book instance")
	}
}

func TestEngine_ApplySnapshot(t *testing.T) {
	engine := NewEngine()
	venueID := uuid.New()
	instrumentID := uuid.New()

	book := engine.GetOrCreateBook(venueID, instrumentID, 5*time.Second, 10*time.Second)

	snapshot := &OrderBookSnapshot{
		VenueID:      venueID,
		InstrumentID: instrumentID,
		Sequence:     1,
		Bids: []PriceLevel{
			{Price: "100", Quantity: "10"},
			{Price: "99", Quantity: "20"},
		},
		Asks: []PriceLevel{
			{Price: "101", Quantity: "5"},
			{Price: "102", Quantity: "15"},
		},
		Timestamp: time.Now(),
	}

	err := engine.ApplySnapshot(book, snapshot)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if book.Sequence != 1 {
		t.Errorf("expected sequence 1, got %d", book.Sequence)
	}
	if len(book.Bids) != 2 {
		t.Errorf("expected 2 bids, got %d", len(book.Bids))
	}
	if len(book.Asks) != 2 {
		t.Errorf("expected 2 asks, got %d", len(book.Asks))
	}
	if book.State != BookStateHealthy {
		t.Errorf("expected state HEALTHY, got %s", book.State)
	}
}

func TestEngine_ApplySnapshot_Sorted(t *testing.T) {
	engine := NewEngine()
	venueID := uuid.New()
	instrumentID := uuid.New()

	book := engine.GetOrCreateBook(venueID, instrumentID, 5*time.Second, 10*time.Second)

	snapshot := &OrderBookSnapshot{
		VenueID:      venueID,
		InstrumentID: instrumentID,
		Sequence:     1,
		Bids: []PriceLevel{
			{Price: "99", Quantity: "20"},
			{Price: "100", Quantity: "10"},
		},
		Asks: []PriceLevel{
			{Price: "102", Quantity: "15"},
			{Price: "101", Quantity: "5"},
		},
		Timestamp: time.Now(),
	}

	engine.ApplySnapshot(book, snapshot)

	if book.Bids[0].Price != "100" {
		t.Errorf("expected best bid 100, got %s", book.Bids[0].Price)
	}
	if book.Asks[0].Price != "101" {
		t.Errorf("expected best ask 101, got %s", book.Asks[0].Price)
	}
}

func TestEngine_ApplyDelta(t *testing.T) {
	engine := NewEngine()
	venueID := uuid.New()
	instrumentID := uuid.New()

	book := engine.GetOrCreateBook(venueID, instrumentID, 5*time.Second, 10*time.Second)

	snapshot := &OrderBookSnapshot{
		VenueID:      venueID,
		InstrumentID: instrumentID,
		Sequence:     1,
		Bids: []PriceLevel{
			{Price: "100", Quantity: "10"},
		},
		Asks: []PriceLevel{
			{Price: "101", Quantity: "5"},
		},
		Timestamp: time.Now(),
	}
	engine.ApplySnapshot(book, snapshot)

	delta := &OrderBookDelta{
		VenueID:      venueID,
		InstrumentID: instrumentID,
		FromSequence: 1,
		ToSequence:   2,
		Bids: []PriceLevel{
			{Price: "100", Quantity: "15"},
			{Price: "99", Quantity: "20"},
		},
		Asks: []PriceLevel{
			{Price: "101", Quantity: "8"},
		},
		Timestamp: time.Now(),
	}

	err := engine.ApplyDelta(book, delta)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if book.Sequence != 2 {
		t.Errorf("expected sequence 2, got %d", book.Sequence)
	}
	if len(book.Bids) != 2 {
		t.Errorf("expected 2 bids, got %d", len(book.Bids))
	}
}

func TestEngine_ApplyDelta_SequenceMismatch(t *testing.T) {
	engine := NewEngine()
	venueID := uuid.New()
	instrumentID := uuid.New()

	book := engine.GetOrCreateBook(venueID, instrumentID, 5*time.Second, 10*time.Second)

	snapshot := &OrderBookSnapshot{
		VenueID:      venueID,
		InstrumentID: instrumentID,
		Sequence:     1,
		Bids: []PriceLevel{
			{Price: "100", Quantity: "10"},
		},
		Asks: []PriceLevel{
			{Price: "101", Quantity: "5"},
		},
		Timestamp: time.Now(),
	}
	engine.ApplySnapshot(book, snapshot)

	delta := &OrderBookDelta{
		VenueID:      venueID,
		InstrumentID: instrumentID,
		FromSequence: 5,
		ToSequence:   6,
		Bids:         []PriceLevel{},
		Asks:         []PriceLevel{},
		Timestamp:    time.Now(),
	}

	err := engine.ApplyDelta(book, delta)
	if err != ErrSequenceMismatch {
		t.Errorf("expected ErrSequenceMismatch, got %v", err)
	}
	if book.State != BookStateDesynced {
		t.Errorf("expected state DESYNCED, got %s", book.State)
	}
}

func TestEngine_ApplyDelta_DeleteLevel(t *testing.T) {
	engine := NewEngine()
	venueID := uuid.New()
	instrumentID := uuid.New()

	book := engine.GetOrCreateBook(venueID, instrumentID, 5*time.Second, 10*time.Second)

	snapshot := &OrderBookSnapshot{
		VenueID:      venueID,
		InstrumentID: instrumentID,
		Sequence:     1,
		Bids: []PriceLevel{
			{Price: "100", Quantity: "10"},
			{Price: "99", Quantity: "20"},
		},
		Asks: []PriceLevel{
			{Price: "101", Quantity: "5"},
		},
		Timestamp: time.Now(),
	}
	engine.ApplySnapshot(book, snapshot)

	delta := &OrderBookDelta{
		VenueID:      venueID,
		InstrumentID: instrumentID,
		FromSequence: 1,
		ToSequence:   2,
		Bids: []PriceLevel{
			{Price: "99", Quantity: "0"},
		},
		Asks:         []PriceLevel{},
		Timestamp:    time.Now(),
	}

	engine.ApplyDelta(book, delta)

	if len(book.Bids) != 1 {
		t.Errorf("expected 1 bid after delete, got %d", len(book.Bids))
	}
}

func TestEngine_CheckFreshness(t *testing.T) {
	engine := NewEngine()
	venueID := uuid.New()
	instrumentID := uuid.New()

	book := engine.GetOrCreateBook(venueID, instrumentID, 1*time.Second, 5*time.Second)

	snapshot := &OrderBookSnapshot{
		VenueID:      venueID,
		InstrumentID: instrumentID,
		Sequence:     1,
		Bids:         []PriceLevel{{Price: "100", Quantity: "10"}},
		Asks:         []PriceLevel{{Price: "101", Quantity: "5"}},
		Timestamp:    time.Now(),
	}
	engine.ApplySnapshot(book, snapshot)

	state := engine.CheckFreshness(book)
	if state != BookStateHealthy {
		t.Errorf("expected HEALTHY, got %s", state)
	}

	book.LastUpdate = time.Now().Add(-2 * time.Second)
	state = engine.CheckFreshness(book)
	if state != BookStateStale {
		t.Errorf("expected STALE, got %s", state)
	}
}

func TestEngine_GetHealth(t *testing.T) {
	engine := NewEngine()
	venueID := uuid.New()
	instrumentID := uuid.New()

	book := engine.GetOrCreateBook(venueID, instrumentID, 5*time.Second, 10*time.Second)

	snapshot := &OrderBookSnapshot{
		VenueID:      venueID,
		InstrumentID: instrumentID,
		Sequence:     1,
		Bids:         []PriceLevel{{Price: "100", Quantity: "10"}},
		Asks:         []PriceLevel{{Price: "101", Quantity: "5"}},
		Timestamp:    time.Now(),
	}
	engine.ApplySnapshot(book, snapshot)

	health := engine.GetHealth(book)

	if health == nil {
		t.Fatal("expected health to be returned")
	}
	if health.VenueID != venueID {
		t.Errorf("expected venue ID %s, got %s", venueID, health.VenueID)
	}
	if !health.IsHealthy {
		t.Error("expected book to be healthy")
	}
}

func TestEngine_RequestResync(t *testing.T) {
	engine := NewEngine()
	venueID := uuid.New()
	instrumentID := uuid.New()

	book := engine.GetOrCreateBook(venueID, instrumentID, 5*time.Second, 10*time.Second)
	book.State = BookStateHealthy

	engine.RequestResync(book)

	if book.State != BookStateResyncing {
		t.Errorf("expected state RESYNCING, got %s", book.State)
	}
	if book.ResyncCount != 1 {
		t.Errorf("expected resync count 1, got %d", book.ResyncCount)
	}
}

func TestEngine_GetDepth(t *testing.T) {
	engine := NewEngine()
	venueID := uuid.New()
	instrumentID := uuid.New()

	book := engine.GetOrCreateBook(venueID, instrumentID, 5*time.Second, 10*time.Second)

	snapshot := &OrderBookSnapshot{
		VenueID:      venueID,
		InstrumentID: instrumentID,
		Sequence:     1,
		Bids: []PriceLevel{
			{Price: "100", Quantity: "10"},
			{Price: "99", Quantity: "20"},
			{Price: "98", Quantity: "30"},
		},
		Asks: []PriceLevel{
			{Price: "101", Quantity: "5"},
			{Price: "102", Quantity: "15"},
			{Price: "103", Quantity: "25"},
		},
		Timestamp: time.Now(),
	}
	engine.ApplySnapshot(book, snapshot)

	depth := engine.GetDepth(book, 2)

	if depth == nil {
		t.Fatal("expected depth to be returned")
	}
	if depth.BestBid == nil || *depth.BestBid != "100" {
		t.Errorf("expected best bid 100, got %v", depth.BestBid)
	}
	if depth.BestAsk == nil || *depth.BestAsk != "101" {
		t.Errorf("expected best ask 101, got %v", depth.BestAsk)
	}
	if len(depth.BidDepth) != 2 {
		t.Errorf("expected 2 bid depth levels, got %d", len(depth.BidDepth))
	}
	if len(depth.AskDepth) != 2 {
		t.Errorf("expected 2 ask depth levels, got %d", len(depth.AskDepth))
	}
}

func TestEngine_IsTradable(t *testing.T) {
	engine := NewEngine()
	venueID := uuid.New()
	instrumentID := uuid.New()

	book := engine.GetOrCreateBook(venueID, instrumentID, 5*time.Second, 10*time.Second)

	if engine.IsTradable(book) {
		t.Error("expected not tradable when empty")
	}

	snapshot := &OrderBookSnapshot{
		VenueID:      venueID,
		InstrumentID: instrumentID,
		Sequence:     1,
		Bids: []PriceLevel{
			{Price: "100", Quantity: "10"},
		},
		Asks: []PriceLevel{
			{Price: "101", Quantity: "5"},
		},
		Timestamp: time.Now(),
	}
	engine.ApplySnapshot(book, snapshot)

	if !engine.IsTradable(book) {
		t.Error("expected tradable after snapshot")
	}

	book.State = BookStateStale
	if engine.IsTradable(book) {
		t.Error("expected not tradable when stale")
	}
}

func TestCalculateSpread(t *testing.T) {
	spread := calculateSpread("100", "101")
	if spread != "1" {
		t.Errorf("expected spread 1, got %s", spread)
	}

	spread = calculateSpread("99.5", "100.5")
	if spread != "1" {
		t.Errorf("expected spread 1, got %s", spread)
	}
}

func TestCalculateNotional(t *testing.T) {
	notional := calculateNotional("100", "10")
	if notional != "1000" {
		t.Errorf("expected notional 1000, got %s", notional)
	}

	notional = calculateNotional("99.5", "0.5")
	if notional != "49.75" {
		t.Errorf("expected notional 49.75, got %s", notional)
	}
}

func TestValidatePriceLevel(t *testing.T) {
	valid := PriceLevel{Price: "100", Quantity: "10"}
	if !ValidatePriceLevel(valid) {
		t.Error("expected valid price level")
	}

	invalid := PriceLevel{Price: "", Quantity: "10"}
	if ValidatePriceLevel(invalid) {
		t.Error("expected invalid price level")
	}

	invalid = PriceLevel{Price: "abc", Quantity: "10"}
	if ValidatePriceLevel(invalid) {
		t.Error("expected invalid price level")
	}
}

func TestValidateSnapshot(t *testing.T) {
	valid := &OrderBookSnapshot{
		Bids: []PriceLevel{{Price: "100", Quantity: "10"}},
		Asks: []PriceLevel{{Price: "101", Quantity: "5"}},
	}
	if !ValidateSnapshot(valid) {
		t.Error("expected valid snapshot")
	}

	invalid := &OrderBookSnapshot{
		Bids: []PriceLevel{{Price: "abc", Quantity: "10"}},
		Asks: []PriceLevel{{Price: "101", Quantity: "5"}},
	}
	if ValidateSnapshot(invalid) {
		t.Error("expected invalid snapshot")
	}
}

func TestValidateDelta(t *testing.T) {
	valid := &OrderBookDelta{
		FromSequence: 1,
		ToSequence:   2,
	}
	if !ValidateDelta(valid) {
		t.Error("expected valid delta")
	}

	invalid := &OrderBookDelta{
		FromSequence: 5,
		ToSequence:   2,
	}
	if ValidateDelta(invalid) {
		t.Error("expected invalid delta")
	}
}

func TestEngine_ApplyDelta_ReplaceLevel(t *testing.T) {
	engine := NewEngine()
	venueID := uuid.New()
	instrumentID := uuid.New()

	book := engine.GetOrCreateBook(venueID, instrumentID, 5*time.Second, 10*time.Second)

	snapshot := &OrderBookSnapshot{
		VenueID:      venueID,
		InstrumentID: instrumentID,
		Sequence:     1,
		Bids: []PriceLevel{
			{Price: "100", Quantity: "10"},
		},
		Asks: []PriceLevel{
			{Price: "101", Quantity: "5"},
		},
		Timestamp: time.Now(),
	}
	engine.ApplySnapshot(book, snapshot)

	delta := &OrderBookDelta{
		VenueID:      venueID,
		InstrumentID: instrumentID,
		FromSequence: 1,
		ToSequence:   2,
		Bids: []PriceLevel{
			{Price: "100", Quantity: "20"},
		},
		Asks:         []PriceLevel{},
		Timestamp:    time.Now(),
	}

	engine.ApplyDelta(book, delta)

	if len(book.Bids) != 1 {
		t.Errorf("expected 1 bid, got %d", len(book.Bids))
	}
	if book.Bids[0].Quantity != "20" {
		t.Errorf("expected quantity 20, got %s", book.Bids[0].Quantity)
	}
}

func TestEngine_GetBestPrices(t *testing.T) {
	engine := NewEngine()
	venueID := uuid.New()
	instrumentID := uuid.New()

	book := engine.GetOrCreateBook(venueID, instrumentID, 5*time.Second, 10*time.Second)

	snapshot := &OrderBookSnapshot{
		VenueID:      venueID,
		InstrumentID: instrumentID,
		Sequence:     1,
		Bids: []PriceLevel{
			{Price: "100", Quantity: "10"},
			{Price: "99", Quantity: "20"},
		},
		Asks: []PriceLevel{
			{Price: "101", Quantity: "5"},
			{Price: "102", Quantity: "15"},
		},
		Timestamp: time.Now(),
	}
	engine.ApplySnapshot(book, snapshot)

	bestBid, bestAsk := engine.GetBestPrices(book)

	if bestBid == nil || *bestBid != "100" {
		t.Errorf("expected best bid 100, got %v", bestBid)
	}
	if bestAsk == nil || *bestAsk != "101" {
		t.Errorf("expected best ask 101, got %v", bestAsk)
	}
}

func TestEngine_ResetBook(t *testing.T) {
	engine := NewEngine()
	venueID := uuid.New()
	instrumentID := uuid.New()

	book := engine.GetOrCreateBook(venueID, instrumentID, 5*time.Second, 10*time.Second)

	snapshot := &OrderBookSnapshot{
		VenueID:      venueID,
		InstrumentID: instrumentID,
		Sequence:     1,
		Bids:         []PriceLevel{{Price: "100", Quantity: "10"}},
		Asks:         []PriceLevel{{Price: "101", Quantity: "5"}},
		Timestamp:    time.Now(),
	}
	engine.ApplySnapshot(book, snapshot)

	engine.ResetBook(book)

	if len(book.Bids) != 0 {
		t.Errorf("expected 0 bids after reset, got %d", len(book.Bids))
	}
	if len(book.Asks) != 0 {
		t.Errorf("expected 0 asks after reset, got %d", len(book.Asks))
	}
	if book.Sequence != 0 {
		t.Errorf("expected sequence 0 after reset, got %d", book.Sequence)
	}
	if book.State != BookStateResyncing {
		t.Errorf("expected state RESYNCING after reset, got %s", book.State)
	}
	if book.ResyncCount != 1 {
		t.Errorf("expected resync count 1, got %d", book.ResyncCount)
	}
}
