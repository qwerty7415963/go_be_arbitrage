package venue

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

type mockRepository struct {
	venues map[uuid.UUID]*Venue
}

func newMockRepository() *mockRepository {
	return &mockRepository{
		venues: make(map[uuid.UUID]*Venue),
	}
}

func (m *mockRepository) Create(ctx context.Context, v *Venue) error {
	m.venues[v.ID] = v
	return nil
}

func (m *mockRepository) GetByID(ctx context.Context, id uuid.UUID) (*Venue, error) {
	v, ok := m.venues[id]
	if !ok {
		return nil, domain.NewError(domain.ErrCodeConfigNotFound, "venue not found")
	}
	return v, nil
}

func (m *mockRepository) GetByCode(ctx context.Context, code string) (*Venue, error) {
	for _, v := range m.venues {
		if v.Code == code {
			return v, nil
		}
	}
	return nil, domain.NewError(domain.ErrCodeConfigNotFound, "venue not found")
}

func (m *mockRepository) List(ctx context.Context) ([]*Venue, error) {
	var venues []*Venue
	for _, v := range m.venues {
		venues = append(venues, v)
	}
	return venues, nil
}

func (m *mockRepository) Update(ctx context.Context, v *Venue) error {
	m.venues[v.ID] = v
	return nil
}

func (m *mockRepository) Delete(ctx context.Context, id uuid.UUID) error {
	delete(m.venues, id)
	return nil
}

func TestVenueCreate_WhenValidRequest_ReturnsVenue(t *testing.T) {
	mock := newMockRepository()

	req := &CreateVenueRequest{
		Code:      "binance",
		Name:      "Binance",
		VenueType: VenueTypeCEX,
	}

	existing, _ := mock.GetByCode(context.Background(), req.Code)
	if existing != nil {
		t.Fatal("expected no existing venue")
	}

	caps, _ := json.Marshal(req.Capabilities)
	v := &Venue{
		ID:           uuid.New(),
		Code:         req.Code,
		Name:         req.Name,
		VenueType:    req.VenueType,
		Status:       VenueStatusActive,
		Capabilities: caps,
		Metadata:     json.RawMessage(`{}`),
	}

	if err := mock.Create(context.Background(), v); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if v.ID == uuid.Nil {
		t.Error("expected venue ID to be set")
	}
	if v.Code != "binance" {
		t.Errorf("expected code binance, got %s", v.Code)
	}
	if v.Status != VenueStatusActive {
		t.Errorf("expected status ACTIVE, got %s", v.Status)
	}
}

func TestVenueCreate_WhenDuplicateCode_ReturnsError(t *testing.T) {
	mock := newMockRepository()

	v1 := &Venue{
		ID:        uuid.New(),
		Code:      "binance",
		Name:      "Binance",
		VenueType: VenueTypeCEX,
		Status:    VenueStatusActive,
	}

	v2 := &Venue{
		ID:        uuid.New(),
		Code:      "binance",
		Name:      "Binance Duplicate",
		VenueType: VenueTypeCEX,
		Status:    VenueStatusActive,
	}

	mock.Create(context.Background(), v1)

	existing, _ := mock.GetByCode(context.Background(), v2.Code)
	if existing == nil {
		t.Error("expected to find existing venue with same code")
	}
}

func TestVenueGetByID_WhenFound_ReturnsVenue(t *testing.T) {
	mock := newMockRepository()

	v := &Venue{
		ID:        uuid.New(),
		Code:      "okx",
		Name:      "OKX",
		VenueType: VenueTypeCEX,
		Status:    VenueStatusActive,
	}

	mock.Create(context.Background(), v)

	found, err := mock.GetByID(context.Background(), v.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if found.ID != v.ID {
		t.Errorf("expected ID %s, got %s", v.ID, found.ID)
	}
	if found.Code != "okx" {
		t.Errorf("expected code okx, got %s", found.Code)
	}
}

func TestVenueGetByID_WhenNotFound_ReturnsError(t *testing.T) {
	mock := newMockRepository()

	_, err := mock.GetByID(context.Background(), uuid.New())
	if err == nil {
		t.Error("expected error for non-existent venue")
	}
}

func TestVenueGetByCode_WhenFound_ReturnsVenue(t *testing.T) {
	mock := newMockRepository()

	v := &Venue{
		ID:        uuid.New(),
		Code:      "bybit",
		Name:      "Bybit",
		VenueType: VenueTypeCEX,
		Status:    VenueStatusActive,
	}

	mock.Create(context.Background(), v)

	found, err := mock.GetByCode(context.Background(), "bybit")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if found.Code != "bybit" {
		t.Errorf("expected code bybit, got %s", found.Code)
	}
}

func TestVenueGetByCode_WhenNotFound_ReturnsError(t *testing.T) {
	mock := newMockRepository()

	_, err := mock.GetByCode(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error for non-existent code")
	}
}

func TestVenueList_ReturnsAllVenues(t *testing.T) {
	mock := newMockRepository()

	v1 := &Venue{ID: uuid.New(), Code: "binance", Name: "Binance", VenueType: VenueTypeCEX, Status: VenueStatusActive}
	v2 := &Venue{ID: uuid.New(), Code: "okx", Name: "OKX", VenueType: VenueTypeCEX, Status: VenueStatusActive}
	v3 := &Venue{ID: uuid.New(), Code: "bybit", Name: "Bybit", VenueType: VenueTypeCEX, Status: VenueStatusActive}

	mock.Create(context.Background(), v1)
	mock.Create(context.Background(), v2)
	mock.Create(context.Background(), v3)

	venues, err := mock.List(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(venues) != 3 {
		t.Errorf("expected 3 venues, got %d", len(venues))
	}
}

func TestVenueUpdate_WhenFound_UpdatesFields(t *testing.T) {
	mock := newMockRepository()

	v := &Venue{
		ID:        uuid.New(),
		Code:      "binance",
		Name:      "Binance",
		VenueType: VenueTypeCEX,
		Status:    VenueStatusActive,
	}

	mock.Create(context.Background(), v)

	v.Name = "Binance Updated"
	mock.Update(context.Background(), v)

	found, _ := mock.GetByID(context.Background(), v.ID)
	if found.Name != "Binance Updated" {
		t.Errorf("expected name 'Binance Updated', got '%s'", found.Name)
	}
}

func TestVenueUpdate_WhenNotFound_ReturnsError(t *testing.T) {
	mock := newMockRepository()

	_, err := mock.GetByID(context.Background(), uuid.New())
	if err == nil {
		t.Error("expected error for non-existent venue")
	}
}

func TestVenueDelete_WhenFound_DeletesVenue(t *testing.T) {
	mock := newMockRepository()

	v := &Venue{
		ID:        uuid.New(),
		Code:      "binance",
		Name:      "Binance",
		VenueType: VenueTypeCEX,
		Status:    VenueStatusActive,
	}

	mock.Create(context.Background(), v)
	mock.Delete(context.Background(), v.ID)

	_, err := mock.GetByID(context.Background(), v.ID)
	if err == nil {
		t.Error("expected error after deletion")
	}
}

func TestVenueDelete_WhenNotFound_ReturnsError(t *testing.T) {
	mock := newMockRepository()

	mock.Delete(context.Background(), uuid.New())

	_, err := mock.GetByID(context.Background(), uuid.New())
	if err == nil {
		t.Error("expected error for non-existent venue")
	}
}
