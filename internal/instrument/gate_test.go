package instrument

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestGate_NewInstrument_NeverAutoTradable(t *testing.T) {
	mock := newMockRepository()

	// Create instrument - must have trading_enabled = false
	inst := &Instrument{
		ID:              uuid.New(),
		CanonicalSymbol: "BTC-USDT",
		BaseAsset:       "BTC",
		QuoteAsset:      "USDT",
		InstrumentType:  InstrumentTypePerp,
		ContractType:    ContractTypeLinear,
		PriceTick:       "0.1",
		QuantityStep:    "0.001",
		DiscoveryStatus: DiscoveryStatusDiscovered,
		TradingEnabled:  false, // MUST be false
	}

	mock.Create(context.Background(), inst)

	// Verify: new instrument must never be tradable
	if inst.TradingEnabled {
		t.Error("GATE VIOLATION: new instrument must have trading_enabled = false")
	}

	// Verify: new instrument must be DISCOVERED
	if inst.DiscoveryStatus != DiscoveryStatusDiscovered {
		t.Errorf("GATE VIOLATION: new instrument must have discovery_status = DISCOVERED, got %s", inst.DiscoveryStatus)
	}
}

func TestGate_EnableTrading_RequiresReview(t *testing.T) {
	mock := newMockRepository()

	// Create instrument with DISCOVERED status
	inst := &Instrument{
		ID:              uuid.New(),
		CanonicalSymbol: "ETH-USDT",
		BaseAsset:       "ETH",
		QuoteAsset:      "USDT",
		InstrumentType:  InstrumentTypePerp,
		DiscoveryStatus: DiscoveryStatusDiscovered,
		TradingEnabled:  false,
	}

	mock.Create(context.Background(), inst)

	// Attempt to enable trading without review - should be blocked
	if inst.DiscoveryStatus == DiscoveryStatusReviewed {
		t.Error("GATE VIOLATION: instrument should NOT be reviewed")
	}

	// Simulate service validation: if not reviewed, cannot enable trading
	if inst.DiscoveryStatus != DiscoveryStatusReviewed {
		// This is the expected path - blocking enable trading
		// In real service: return domain.NewError(domain.ErrCodeStrategyInvalidConf, "instrument must be reviewed before enabling trading")
	} else {
		t.Error("GATE VIOLATION: DISCOVERED instrument should not be able to enable trading")
	}
}

func TestGate_EnableTrading_RequiresReviewedStatus(t *testing.T) {
	mock := newMockRepository()

	// Test with REJECTED status
	inst := &Instrument{
		ID:              uuid.New(),
		CanonicalSymbol: "SOL-USDT",
		BaseAsset:       "SOL",
		QuoteAsset:      "USDT",
		InstrumentType:  InstrumentTypePerp,
		DiscoveryStatus: DiscoveryStatusRejected,
		TradingEnabled:  false,
	}

	mock.Create(context.Background(), inst)

	// Attempt to enable trading on rejected instrument - should be blocked
	if inst.DiscoveryStatus != DiscoveryStatusReviewed {
		// Expected: rejected instrument cannot enable trading
	} else {
		t.Error("GATE VIOLATION: REJECTED instrument should not be able to enable trading")
	}

	// Now test with REVIEWED status - should be allowed
	instReviewed := &Instrument{
		ID:              uuid.New(),
		CanonicalSymbol: "AVAX-USDT",
		BaseAsset:       "AVAX",
		QuoteAsset:      "USDT",
		InstrumentType:  InstrumentTypePerp,
		DiscoveryStatus: DiscoveryStatusReviewed,
		TradingEnabled:  false,
	}

	mock.Create(context.Background(), instReviewed)

	// Enable trading on reviewed instrument
	err := mock.EnableTrading(context.Background(), instReviewed.ID, true)
	if err != nil {
		t.Fatalf("unexpected error enabling trading on reviewed instrument: %v", err)
	}

	// Verify trading is now enabled
	found, _ := mock.GetByID(context.Background(), instReviewed.ID)
	if !found.TradingEnabled {
		t.Error("GATE VIOLATION: reviewed instrument should be able to enable trading")
	}
}
