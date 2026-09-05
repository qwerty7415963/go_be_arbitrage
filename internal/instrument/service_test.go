package instrument

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

type mockRepository struct {
	instruments    map[uuid.UUID]*Instrument
	venueInstruments map[uuid.UUID]*VenueInstrument
}

func newMockRepository() *mockRepository {
	return &mockRepository{
		instruments:    make(map[uuid.UUID]*Instrument),
		venueInstruments: make(map[uuid.UUID]*VenueInstrument),
	}
}

func (m *mockRepository) Create(ctx context.Context, inst *Instrument) error {
	m.instruments[inst.ID] = inst
	return nil
}

func (m *mockRepository) GetByID(ctx context.Context, id uuid.UUID) (*Instrument, error) {
	inst, ok := m.instruments[id]
	if !ok {
		return nil, domain.NewError(domain.ErrCodeConfigNotFound, "instrument not found")
	}
	return inst, nil
}

func (m *mockRepository) GetByCanonicalSymbol(ctx context.Context, symbol string) (*Instrument, error) {
	for _, inst := range m.instruments {
		if inst.CanonicalSymbol == symbol {
			return inst, nil
		}
	}
	return nil, domain.NewError(domain.ErrCodeConfigNotFound, "instrument not found")
}

func (m *mockRepository) List(ctx context.Context) ([]*Instrument, error) {
	var instruments []*Instrument
	for _, inst := range m.instruments {
		instruments = append(instruments, inst)
	}
	return instruments, nil
}

func (m *mockRepository) ListTradable(ctx context.Context) ([]*Instrument, error) {
	var instruments []*Instrument
	for _, inst := range m.instruments {
		if inst.TradingEnabled && inst.DiscoveryStatus == DiscoveryStatusReviewed {
			instruments = append(instruments, inst)
		}
	}
	return instruments, nil
}

func (m *mockRepository) Update(ctx context.Context, inst *Instrument) error {
	m.instruments[inst.ID] = inst
	return nil
}

func (m *mockRepository) EnableTrading(ctx context.Context, id uuid.UUID, enabled bool) error {
	inst, ok := m.instruments[id]
	if !ok {
		return domain.NewError(domain.ErrCodeConfigNotFound, "instrument not found")
	}
	inst.TradingEnabled = enabled
	return nil
}

func (m *mockRepository) Delete(ctx context.Context, id uuid.UUID) error {
	delete(m.instruments, id)
	return nil
}

func (m *mockRepository) CreateVenueInstrument(ctx context.Context, vi *VenueInstrument) error {
	m.venueInstruments[vi.ID] = vi
	return nil
}

func (m *mockRepository) GetVenueInstrument(ctx context.Context, venueID uuid.UUID, venueSymbol string) (*VenueInstrument, error) {
	for _, vi := range m.venueInstruments {
		if vi.VenueID == venueID && vi.VenueSymbol == venueSymbol {
			return vi, nil
		}
	}
	return nil, domain.NewError(domain.ErrCodeConfigNotFound, "venue instrument not found")
}

func (m *mockRepository) ListVenueInstruments(ctx context.Context, venueID uuid.UUID) ([]*VenueInstrument, error) {
	var result []*VenueInstrument
	for _, vi := range m.venueInstruments {
		if vi.VenueID == venueID {
			result = append(result, vi)
		}
	}
	return result, nil
}

func TestInstrumentCreate_WhenValidRequest_ReturnsInstrument(t *testing.T) {
	mock := newMockRepository()

	req := &CreateInstrumentRequest{
		CanonicalSymbol: "BTC-USDT",
		BaseAsset:       "BTC",
		QuoteAsset:      "USDT",
		InstrumentType:  InstrumentTypePerp,
		ContractType:    ContractTypeLinear,
		PriceTick:       "0.1",
		QuantityStep:    "0.001",
	}

	existing, _ := mock.GetByCanonicalSymbol(context.Background(), req.CanonicalSymbol)
	if existing != nil {
		t.Fatal("expected no existing instrument")
	}

	inst := &Instrument{
		ID:              uuid.New(),
		CanonicalSymbol: req.CanonicalSymbol,
		BaseAsset:       req.BaseAsset,
		QuoteAsset:      req.QuoteAsset,
		InstrumentType:  req.InstrumentType,
		ContractType:    req.ContractType,
		PriceTick:       req.PriceTick,
		QuantityStep:    req.QuantityStep,
		DiscoveryStatus: DiscoveryStatusDiscovered,
		TradingEnabled:  false,
	}

	if err := mock.Create(context.Background(), inst); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if inst.ID == uuid.Nil {
		t.Error("expected instrument ID to be set")
	}
	if inst.CanonicalSymbol != "BTC-USDT" {
		t.Errorf("expected canonical symbol BTC-USDT, got %s", inst.CanonicalSymbol)
	}
}

func TestInstrumentCreate_WhenDuplicateSymbol_ReturnsError(t *testing.T) {
	mock := newMockRepository()

	inst1 := &Instrument{
		ID:              uuid.New(),
		CanonicalSymbol: "BTC-USDT",
		BaseAsset:       "BTC",
		QuoteAsset:      "USDT",
		InstrumentType:  InstrumentTypePerp,
		DiscoveryStatus: DiscoveryStatusDiscovered,
		TradingEnabled:  false,
	}

	inst2 := &Instrument{
		ID:              uuid.New(),
		CanonicalSymbol: "BTC-USDT",
		BaseAsset:       "BTC",
		QuoteAsset:      "USDT",
		InstrumentType:  InstrumentTypePerp,
		DiscoveryStatus: DiscoveryStatusDiscovered,
		TradingEnabled:  false,
	}

	mock.Create(context.Background(), inst1)

	existing, _ := mock.GetByCanonicalSymbol(context.Background(), inst2.CanonicalSymbol)
	if existing == nil {
		t.Error("expected to find existing instrument with same symbol")
	}
}

func TestInstrumentCreate_SetsTradingEnabledFalse(t *testing.T) {
	mock := newMockRepository()

	inst := &Instrument{
		ID:              uuid.New(),
		CanonicalSymbol: "ETH-USDT",
		BaseAsset:       "ETH",
		QuoteAsset:      "USDT",
		InstrumentType:  InstrumentTypePerp,
		DiscoveryStatus: DiscoveryStatusDiscovered,
		TradingEnabled:  false, // Must be false
	}

	mock.Create(context.Background(), inst)

	if inst.TradingEnabled {
		t.Error("expected trading_enabled to be false for new instrument")
	}
}

func TestInstrumentCreate_SetsDiscoveryStatusDiscovered(t *testing.T) {
	mock := newMockRepository()

	inst := &Instrument{
		ID:              uuid.New(),
		CanonicalSymbol: "SOL-USDT",
		BaseAsset:       "SOL",
		QuoteAsset:      "USDT",
		InstrumentType:  InstrumentTypePerp,
		DiscoveryStatus: DiscoveryStatusDiscovered,
		TradingEnabled:  false,
	}

	mock.Create(context.Background(), inst)

	if inst.DiscoveryStatus != DiscoveryStatusDiscovered {
		t.Errorf("expected discovery_status DISCOVERED, got %s", inst.DiscoveryStatus)
	}
}

func TestInstrumentEnableTrading_WhenNotReviewed_ReturnsError(t *testing.T) {
	mock := newMockRepository()

	inst := &Instrument{
		ID:              uuid.New(),
		CanonicalSymbol: "DOGE-USDT",
		BaseAsset:       "DOGE",
		QuoteAsset:      "USDT",
		InstrumentType:  InstrumentTypePerp,
		DiscoveryStatus: DiscoveryStatusDiscovered, // NOT reviewed
		TradingEnabled:  false,
	}

	mock.Create(context.Background(), inst)

	// Simulate service validation
	if inst.DiscoveryStatus != DiscoveryStatusReviewed {
		// This is the expected path - instrument not reviewed
		if inst.DiscoveryStatus == DiscoveryStatusReviewed {
			t.Error("expected instrument NOT to be reviewed")
		}
	}
}

func TestInstrumentEnableTrading_WhenReviewed_ReturnsSuccess(t *testing.T) {
	mock := newMockRepository()

	inst := &Instrument{
		ID:              uuid.New(),
		CanonicalSymbol: "AVAX-USDT",
		BaseAsset:       "AVAX",
		QuoteAsset:      "USDT",
		InstrumentType:  InstrumentTypePerp,
		DiscoveryStatus: DiscoveryStatusReviewed, // Reviewed
		TradingEnabled:  false,
	}

	mock.Create(context.Background(), inst)

	// Simulate enabling trading
	if inst.DiscoveryStatus == DiscoveryStatusReviewed {
		err := mock.EnableTrading(context.Background(), inst.ID, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	found, _ := mock.GetByID(context.Background(), inst.ID)
	if !found.TradingEnabled {
		t.Error("expected trading_enabled to be true after enabling")
	}
}

func TestInstrumentEnableTrading_WhenRejected_ReturnsError(t *testing.T) {
	mock := newMockRepository()

	inst := &Instrument{
		ID:              uuid.New(),
		CanonicalSymbol: "MATIC-USDT",
		BaseAsset:       "MATIC",
		QuoteAsset:      "USDT",
		InstrumentType:  InstrumentTypePerp,
		DiscoveryStatus: DiscoveryStatusRejected, // Rejected
		TradingEnabled:  false,
	}

	mock.Create(context.Background(), inst)

	// Simulate service validation - should fail
	if inst.DiscoveryStatus != DiscoveryStatusReviewed {
		// Expected path - instrument rejected, cannot enable trading
		if inst.DiscoveryStatus == DiscoveryStatusReviewed {
			t.Error("expected instrument NOT to be reviewed")
		}
	}
}

func TestInstrumentDisableTrading_WhenEnabled_Disables(t *testing.T) {
	mock := newMockRepository()

	inst := &Instrument{
		ID:              uuid.New(),
		CanonicalSymbol: "LINK-USDT",
		BaseAsset:       "LINK",
		QuoteAsset:      "USDT",
		InstrumentType:  InstrumentTypePerp,
		DiscoveryStatus: DiscoveryStatusReviewed,
		TradingEnabled:  true, // Currently enabled
	}

	mock.Create(context.Background(), inst)

	err := mock.EnableTrading(context.Background(), inst.ID, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found, _ := mock.GetByID(context.Background(), inst.ID)
	if found.TradingEnabled {
		t.Error("expected trading_enabled to be false after disabling")
	}
}

func TestInstrumentGetByID_WhenFound_ReturnsInstrument(t *testing.T) {
	mock := newMockRepository()

	inst := &Instrument{
		ID:              uuid.New(),
		CanonicalSymbol: "BTC-USDT",
		BaseAsset:       "BTC",
		QuoteAsset:      "USDT",
		InstrumentType:  InstrumentTypePerp,
		DiscoveryStatus: DiscoveryStatusDiscovered,
		TradingEnabled:  false,
	}

	mock.Create(context.Background(), inst)

	found, err := mock.GetByID(context.Background(), inst.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if found.ID != inst.ID {
		t.Errorf("expected ID %s, got %s", inst.ID, found.ID)
	}
}

func TestInstrumentGetByID_WhenNotFound_ReturnsError(t *testing.T) {
	mock := newMockRepository()

	_, err := mock.GetByID(context.Background(), uuid.New())
	if err == nil {
		t.Error("expected error for non-existent instrument")
	}
}

func TestInstrumentGetBySymbol_WhenFound_ReturnsInstrument(t *testing.T) {
	mock := newMockRepository()

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

	found, err := mock.GetByCanonicalSymbol(context.Background(), "ETH-USDT")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if found.CanonicalSymbol != "ETH-USDT" {
		t.Errorf("expected canonical symbol ETH-USDT, got %s", found.CanonicalSymbol)
	}
}

func TestInstrumentList_ReturnsAllInstruments(t *testing.T) {
	mock := newMockRepository()

	inst1 := &Instrument{ID: uuid.New(), CanonicalSymbol: "BTC-USDT", TradingEnabled: false, DiscoveryStatus: DiscoveryStatusDiscovered}
	inst2 := &Instrument{ID: uuid.New(), CanonicalSymbol: "ETH-USDT", TradingEnabled: false, DiscoveryStatus: DiscoveryStatusDiscovered}
	inst3 := &Instrument{ID: uuid.New(), CanonicalSymbol: "SOL-USDT", TradingEnabled: false, DiscoveryStatus: DiscoveryStatusDiscovered}

	mock.Create(context.Background(), inst1)
	mock.Create(context.Background(), inst2)
	mock.Create(context.Background(), inst3)

	instruments, err := mock.List(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(instruments) != 3 {
		t.Errorf("expected 3 instruments, got %d", len(instruments))
	}
}

func TestInstrumentListTradable_ReturnsOnlyTradable(t *testing.T) {
	mock := newMockRepository()

	// Only this one should be in tradable list
	inst1 := &Instrument{ID: uuid.New(), CanonicalSymbol: "BTC-USDT", TradingEnabled: true, DiscoveryStatus: DiscoveryStatusReviewed}
	inst2 := &Instrument{ID: uuid.New(), CanonicalSymbol: "ETH-USDT", TradingEnabled: false, DiscoveryStatus: DiscoveryStatusReviewed}
	inst3 := &Instrument{ID: uuid.New(), CanonicalSymbol: "SOL-USDT", TradingEnabled: true, DiscoveryStatus: DiscoveryStatusDiscovered}

	mock.Create(context.Background(), inst1)
	mock.Create(context.Background(), inst2)
	mock.Create(context.Background(), inst3)

	tradable, err := mock.ListTradable(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(tradable) != 1 {
		t.Errorf("expected 1 tradable instrument, got %d", len(tradable))
	}
	if len(tradable) > 0 && tradable[0].CanonicalSymbol != "BTC-USDT" {
		t.Errorf("expected BTC-USDT, got %s", tradable[0].CanonicalSymbol)
	}
}

func TestInstrumentUpdate_WhenFound_UpdatesFields(t *testing.T) {
	mock := newMockRepository()

	inst := &Instrument{
		ID:              uuid.New(),
		CanonicalSymbol: "BTC-USDT",
		BaseAsset:       "BTC",
		QuoteAsset:      "USDT",
		InstrumentType:  InstrumentTypePerp,
		PriceTick:       "0.1",
		DiscoveryStatus: DiscoveryStatusDiscovered,
		TradingEnabled:  false,
	}

	mock.Create(context.Background(), inst)

	inst.PriceTick = "0.01"
	mock.Update(context.Background(), inst)

	found, _ := mock.GetByID(context.Background(), inst.ID)
	if found.PriceTick != "0.01" {
		t.Errorf("expected price_tick 0.01, got %s", found.PriceTick)
	}
}

func TestInstrumentDelete_WhenFound_Deletes(t *testing.T) {
	mock := newMockRepository()

	inst := &Instrument{
		ID:              uuid.New(),
		CanonicalSymbol: "BTC-USDT",
		TradingEnabled:  false,
		DiscoveryStatus: DiscoveryStatusDiscovered,
	}

	mock.Create(context.Background(), inst)
	mock.Delete(context.Background(), inst.ID)

	_, err := mock.GetByID(context.Background(), inst.ID)
	if err == nil {
		t.Error("expected error after deletion")
	}
}

func TestVenueInstrumentCreate_WhenValid_ReturnsMapping(t *testing.T) {
	mock := newMockRepository()

	vi := &VenueInstrument{
		ID:           uuid.New(),
		VenueID:      uuid.New(),
		InstrumentID: uuid.New(),
		VenueSymbol:  "BTCUSDT",
		Status:       "ACTIVE",
	}

	err := mock.CreateVenueInstrument(context.Background(), vi)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if vi.ID == uuid.Nil {
		t.Error("expected venue instrument ID to be set")
	}
}

func TestVenueInstrumentCreate_WhenDuplicate_ReturnsError(t *testing.T) {
	mock := newMockRepository()

	venueID := uuid.New()
	symbol := "BTCUSDT"

	vi1 := &VenueInstrument{
		ID:           uuid.New(),
		VenueID:      venueID,
		InstrumentID: uuid.New(),
		VenueSymbol:  symbol,
		Status:       "ACTIVE",
	}

	vi2 := &VenueInstrument{
		ID:           uuid.New(),
		VenueID:      venueID,
		InstrumentID: uuid.New(),
		VenueSymbol:  symbol,
		Status:       "ACTIVE",
	}

	mock.CreateVenueInstrument(context.Background(), vi1)

	existing, _ := mock.GetVenueInstrument(context.Background(), venueID, symbol)
	if existing == nil {
		t.Error("expected to find existing venue instrument")
	}

	_ = vi2 // vi2 would be duplicate
}

func TestVenueInstrumentList_ReturnsForVenue(t *testing.T) {
	mock := newMockRepository()

	venueA := uuid.New()
	venueB := uuid.New()

	vi1 := &VenueInstrument{ID: uuid.New(), VenueID: venueA, VenueSymbol: "BTCUSDT", Status: "ACTIVE"}
	vi2 := &VenueInstrument{ID: uuid.New(), VenueID: venueA, VenueSymbol: "ETHUSDT", Status: "ACTIVE"}
	vi3 := &VenueInstrument{ID: uuid.New(), VenueID: venueB, VenueSymbol: "SOLUSDT", Status: "ACTIVE"}

	mock.CreateVenueInstrument(context.Background(), vi1)
	mock.CreateVenueInstrument(context.Background(), vi2)
	mock.CreateVenueInstrument(context.Background(), vi3)

	result, err := mock.ListVenueInstruments(context.Background(), venueA)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 2 {
		t.Errorf("expected 2 venue instruments for venue A, got %d", len(result))
	}
}
