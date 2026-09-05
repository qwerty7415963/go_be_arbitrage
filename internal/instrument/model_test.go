package instrument

import (
	"testing"

	"github.com/google/uuid"
)

func TestInstrumentModel(t *testing.T) {
	t.Run("instrument types", func(t *testing.T) {
		if InstrumentTypeSpot != "SPOT" {
			t.Errorf("expected SPOT, got %s", InstrumentTypeSpot)
		}
		if InstrumentTypePerp != "PERP" {
			t.Errorf("expected PERP, got %s", InstrumentTypePerp)
		}
		if InstrumentTypeFuture != "FUTURE" {
			t.Errorf("expected FUTURE, got %s", InstrumentTypeFuture)
		}
	})

	t.Run("contract types", func(t *testing.T) {
		if ContractTypeLinear != "LINEAR" {
			t.Errorf("expected LINEAR, got %s", ContractTypeLinear)
		}
		if ContractTypeInverse != "INVERSE" {
			t.Errorf("expected INVERSE, got %s", ContractTypeInverse)
		}
		if ContractTypeSpot != "SPOT" {
			t.Errorf("expected SPOT, got %s", ContractTypeSpot)
		}
		if ContractTypeOther != "OTHER" {
			t.Errorf("expected OTHER, got %s", ContractTypeOther)
		}
	})

	t.Run("discovery statuses", func(t *testing.T) {
		if DiscoveryStatusDiscovered != "DISCOVERED" {
			t.Errorf("expected DISCOVERED, got %s", DiscoveryStatusDiscovered)
		}
		if DiscoveryStatusReviewed != "REVIEWED" {
			t.Errorf("expected REVIEWED, got %s", DiscoveryStatusReviewed)
		}
		if DiscoveryStatusRejected != "REJECTED" {
			t.Errorf("expected REJECTED, got %s", DiscoveryStatusRejected)
		}
	})

	t.Run("instrument creation", func(t *testing.T) {
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
			TradingEnabled:  false,
		}

		if inst.CanonicalSymbol != "BTC-USDT" {
			t.Errorf("expected canonical symbol BTC-USDT, got %s", inst.CanonicalSymbol)
		}
		if inst.TradingEnabled {
			t.Error("expected trading_enabled to be false by default")
		}
		if inst.DiscoveryStatus != DiscoveryStatusDiscovered {
			t.Errorf("expected discovery_status DISCOVERED, got %s", inst.DiscoveryStatus)
		}
	})
}

func TestCreateInstrumentRequest(t *testing.T) {
	req := &CreateInstrumentRequest{
		CanonicalSymbol: "ETH-USDT",
		BaseAsset:       "ETH",
		QuoteAsset:      "USDT",
		InstrumentType:  InstrumentTypePerp,
		ContractType:    ContractTypeLinear,
		PriceTick:       "0.01",
		QuantityStep:    "0.01",
	}

	if req.CanonicalSymbol != "ETH-USDT" {
		t.Errorf("expected canonical symbol ETH-USDT, got %s", req.CanonicalSymbol)
	}
	if req.InstrumentType != InstrumentTypePerp {
		t.Errorf("expected instrument type PERP, got %s", req.InstrumentType)
	}
}

func TestVenueInstrument(t *testing.T) {
	vi := &VenueInstrument{
		ID:            uuid.New(),
		VenueID:       uuid.New(),
		InstrumentID:  uuid.New(),
		VenueSymbol:   "BTCUSDT",
		Status:        "ACTIVE",
		VenueMetadata: map[string]interface{}{},
	}

	if vi.VenueSymbol != "BTCUSDT" {
		t.Errorf("expected venue symbol BTCUSDT, got %s", vi.VenueSymbol)
	}
	if vi.Status != "ACTIVE" {
		t.Errorf("expected status ACTIVE, got %s", vi.Status)
	}
}
