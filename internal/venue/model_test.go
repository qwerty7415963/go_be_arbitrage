package venue

import (
	"testing"

	"github.com/google/uuid"
)

func TestVenueModel(t *testing.T) {
	t.Run("venue types", func(t *testing.T) {
		if VenueTypeCEX != "CEX" {
			t.Errorf("expected CEX, got %s", VenueTypeCEX)
		}
		if VenueTypePerpDEX != "PERP_DEX" {
			t.Errorf("expected PERP_DEX, got %s", VenueTypePerpDEX)
		}
	})

	t.Run("venue statuses", func(t *testing.T) {
		if VenueStatusActive != "ACTIVE" {
			t.Errorf("expected ACTIVE, got %s", VenueStatusActive)
		}
		if VenueStatusDisabled != "DISABLED" {
			t.Errorf("expected DISABLED, got %s", VenueStatusDisabled)
		}
	})

	t.Run("venue creation", func(t *testing.T) {
		v := &Venue{
			ID:        uuid.New(),
			Code:      "binance",
			Name:      "Binance",
			VenueType: VenueTypeCEX,
			Status:    VenueStatusActive,
		}

		if v.Code != "binance" {
			t.Errorf("expected code binance, got %s", v.Code)
		}
		if v.VenueType != VenueTypeCEX {
			t.Errorf("expected venue type CEX, got %s", v.VenueType)
		}
	})
}

func TestCapabilities(t *testing.T) {
	caps := Capabilities{
		SupportsSpot:    true,
		SupportsPerp:    true,
		SupportsFutures: false,
		HasWS:          true,
		HasREST:        true,
	}

	if !caps.SupportsSpot {
		t.Error("expected supports_spot to be true")
	}
	if !caps.SupportsPerp {
		t.Error("expected supports_perp to be true")
	}
	if caps.SupportsFutures {
		t.Error("expected supports_futures to be false")
	}
}

func TestCreateVenueRequest(t *testing.T) {
	req := &CreateVenueRequest{
		Code:      "okx",
		Name:      "OKX",
		VenueType: VenueTypeCEX,
		Capabilities: Capabilities{
			SupportsSpot: true,
			SupportsPerp: true,
		},
	}

	if req.Code != "okx" {
		t.Errorf("expected code okx, got %s", req.Code)
	}
	if req.VenueType != VenueTypeCEX {
		t.Errorf("expected venue type CEX, got %s", req.VenueType)
	}
}
