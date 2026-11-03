package fundingarbitrage

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type mockFundingRepo struct {
	venues     []VenuePerp
	mappings   []VenueInstrument
	funding    map[string]*FundingRecord
	fundingErr map[string]error
}

func newMockFundingRepo() *mockFundingRepo {
	return &mockFundingRepo{
		funding:    make(map[string]*FundingRecord),
		fundingErr: make(map[string]error),
	}
}

func fundingKey(venueID, instrumentID uuid.UUID) string {
	return venueID.String() + ":" + instrumentID.String()
}

func (m *mockFundingRepo) GetPerpVenues(ctx context.Context) ([]VenuePerp, error) {
	return m.venues, nil
}

func (m *mockFundingRepo) GetVenuesByIDs(ctx context.Context, ids []uuid.UUID) ([]VenuePerp, error) {
	var out []VenuePerp
	for _, id := range ids {
		for _, v := range m.venues {
			if v.ID == id {
				out = append(out, v)
			}
		}
	}
	return out, nil
}

func (m *mockFundingRepo) GetVenueInstrumentMappings(ctx context.Context, venueIDs []uuid.UUID) ([]VenueInstrument, error) {
	return m.mappings, nil
}

func (m *mockFundingRepo) GetLatestFunding(ctx context.Context, venueID, instrumentID uuid.UUID) (*FundingRecord, error) {
	if err, ok := m.fundingErr[fundingKey(venueID, instrumentID)]; ok && err != nil {
		return nil, err
	}
	if fr, ok := m.funding[fundingKey(venueID, instrumentID)]; ok {
		return fr, nil
	}
	return nil, errors.New("not found")
}

func TestBuildArbitrageToken_MissingFundingA_ReturnsNil(t *testing.T) {
	repo := newMockFundingRepo()
	svc := NewService(repo, NewCache(0))
	ctx := context.Background()

	idA := uuid.New()
	idB := uuid.New()
	inst := uuid.New()

	vA := VenueInstrument{VenueID: idA, InstrumentID: inst, BaseAsset: "BTC", VenueSymbol: "BTC-PERP"}
	vB := VenueInstrument{VenueID: idB, InstrumentID: inst, BaseAsset: "BTC", VenueSymbol: "BTC-PERP"}

	repo.fundingErr[idA.String()+":"+inst.String()] = errors.New("missing")
	frB := &FundingRecord{VenueID: idB, InstrumentID: inst, ObservedAt: time.Now(), FundingRate: "0.0001", IntervalSeconds: 28800, MarkPrice: "100"}
	repo.funding[fundingKey(idB, inst)] = frB

	token := svc.buildArbitrageToken(ctx, inst, vA, vB, false)
	if token != nil {
		t.Fatalf("expected nil, got %+v", token)
	}
}

func TestBuildArbitrageToken_MissingFundingB_ReturnsNil(t *testing.T) {
	repo := newMockFundingRepo()
	svc := NewService(repo, NewCache(0))
	ctx := context.Background()

	idA := uuid.New()
	idB := uuid.New()
	inst := uuid.New()

	vA := VenueInstrument{VenueID: idA, InstrumentID: inst, BaseAsset: "BTC", VenueSymbol: "BTC-PERP"}
	vB := VenueInstrument{VenueID: idB, InstrumentID: inst, BaseAsset: "BTC", VenueSymbol: "BTC-PERP"}

	frA := &FundingRecord{VenueID: idA, InstrumentID: inst, ObservedAt: time.Now(), FundingRate: "0.0001", IntervalSeconds: 28800, MarkPrice: "100"}
	repo.funding[fundingKey(idA, inst)] = frA
	repo.fundingErr[fundingKey(idB, inst)] = errors.New("missing")

	token := svc.buildArbitrageToken(ctx, inst, vA, vB, false)
	if token != nil {
		t.Fatalf("expected nil, got %+v", token)
	}
}

func TestGetFundingArbitrage_ExcludesUnavailableTokens(t *testing.T) {
	repo := newMockFundingRepo()
	cache := NewCache(0)
	svc := NewService(repo, cache)
	ctx := context.Background()

	idA := uuid.New()
	idB := uuid.New()
	repo.venues = []VenuePerp{
		{ID: idA, Code: "A", Name: "VenueA", VenueType: "CEX"},
		{ID: idB, Code: "B", Name: "VenueB", VenueType: "CEX"},
	}

	inst1 := uuid.New()
	inst2 := uuid.New()
	repo.mappings = []VenueInstrument{
		{VenueID: idA, InstrumentID: inst1, BaseAsset: "BTC", VenueSymbol: "BTC-PERP"},
		{VenueID: idB, InstrumentID: inst1, BaseAsset: "BTC", VenueSymbol: "BTC-PERP"},
		{VenueID: idA, InstrumentID: inst2, BaseAsset: "ETH", VenueSymbol: "ETH-PERP"},
		{VenueID: idB, InstrumentID: inst2, BaseAsset: "ETH", VenueSymbol: "ETH-PERP"},
	}

	now := time.Now()
	repo.funding[fundingKey(idA, inst1)] = &FundingRecord{VenueID: idA, InstrumentID: inst1, ObservedAt: now, FundingRate: "0.0001", IntervalSeconds: 28800, MarkPrice: "100"}
	repo.funding[fundingKey(idB, inst1)] = &FundingRecord{VenueID: idB, InstrumentID: inst1, ObservedAt: now, FundingRate: "0.0002", IntervalSeconds: 28800, MarkPrice: "101"}
	repo.funding[fundingKey(idA, inst2)] = &FundingRecord{VenueID: idA, InstrumentID: inst2, ObservedAt: now, FundingRate: "0.0003", IntervalSeconds: 28800, MarkPrice: "200"}
	// inst2 venue B missing -> unavailable -> dropped

	resp, err := svc.GetFundingArbitrage(ctx, []uuid.UUID{idA, idB}, "rate_8h_desc", false, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Pairs) != 1 {
		t.Fatalf("expected 1 pair, got %d", len(resp.Pairs))
	}
	if len(resp.Pairs[0].Tokens) != 1 {
		t.Fatalf("expected 1 token (only available), got %d", len(resp.Pairs[0].Tokens))
	}
	if resp.Pairs[0].Tokens[0].Symbol != "BTC-PERP" {
		t.Errorf("expected BTC-PERP, got %s", resp.Pairs[0].Tokens[0].Symbol)
	}
}
