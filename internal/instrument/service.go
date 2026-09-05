package instrument

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) Create(ctx context.Context, req *CreateInstrumentRequest) (*Instrument, error) {
	existing, _ := s.repo.GetByCanonicalSymbol(ctx, req.CanonicalSymbol)
	if existing != nil {
		return nil, domain.NewError(domain.ErrCodeConfigInvalid, "canonical symbol already exists")
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
		ContractSize:    req.ContractSize,
		MinQuantity:     req.MinQuantity,
		MinNotional:     req.MinNotional,
		MarginAsset:     req.MarginAsset,
		SettlementAsset: req.SettlementAsset,
		DiscoveryStatus: DiscoveryStatusDiscovered,
		TradingEnabled:  false, // Never auto-enable trading
	}

	if err := s.repo.Create(ctx, inst); err != nil {
		return nil, domain.WrapError(domain.ErrCodeConfigInvalid, "failed to create instrument", err)
	}

	return inst, nil
}

func (s *Service) GetByID(ctx context.Context, id uuid.UUID) (*Instrument, error) {
	inst, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, domain.WrapError(domain.ErrCodeConfigNotFound, "instrument not found", err)
	}
	return inst, nil
}

func (s *Service) GetByCanonicalSymbol(ctx context.Context, symbol string) (*Instrument, error) {
	inst, err := s.repo.GetByCanonicalSymbol(ctx, symbol)
	if err != nil {
		return nil, domain.WrapError(domain.ErrCodeConfigNotFound, "instrument not found", err)
	}
	return inst, nil
}

func (s *Service) List(ctx context.Context) ([]*Instrument, error) {
	return s.repo.List(ctx)
}

func (s *Service) ListTradable(ctx context.Context) ([]*Instrument, error) {
	return s.repo.ListTradable(ctx)
}

func (s *Service) Update(ctx context.Context, id uuid.UUID, req *UpdateInstrumentRequest) (*Instrument, error) {
	inst, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, domain.WrapError(domain.ErrCodeConfigNotFound, "instrument not found", err)
	}

	if req.PriceTick != nil {
		inst.PriceTick = *req.PriceTick
	}
	if req.QuantityStep != nil {
		inst.QuantityStep = *req.QuantityStep
	}
	if req.ContractSize != nil {
		inst.ContractSize = req.ContractSize
	}
	if req.MinQuantity != nil {
		inst.MinQuantity = req.MinQuantity
	}
	if req.MinNotional != nil {
		inst.MinNotional = req.MinNotional
	}
	if req.MarginAsset != nil {
		inst.MarginAsset = req.MarginAsset
	}
	if req.SettlementAsset != nil {
		inst.SettlementAsset = req.SettlementAsset
	}
	if req.DiscoveryStatus != nil {
		inst.DiscoveryStatus = *req.DiscoveryStatus
	}

	if err := s.repo.Update(ctx, inst); err != nil {
		return nil, domain.WrapError(domain.ErrCodeConfigInvalid, "failed to update instrument", err)
	}

	return inst, nil
}

func (s *Service) EnableTrading(ctx context.Context, id uuid.UUID, enabled bool) error {
	inst, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return domain.WrapError(domain.ErrCodeConfigNotFound, "instrument not found", err)
	}

	if enabled && inst.DiscoveryStatus != DiscoveryStatusReviewed {
		return domain.NewError(domain.ErrCodeStrategyInvalidConf, "instrument must be reviewed before enabling trading")
	}

	return s.repo.EnableTrading(ctx, id, enabled)
}

func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return domain.WrapError(domain.ErrCodeConfigNotFound, "instrument not found", err)
	}

	return s.repo.Delete(ctx, id)
}

func (s *Service) CreateVenueInstrument(ctx context.Context, req *CreateVenueInstrumentRequest) (*VenueInstrument, error) {
	existing, _ := s.repo.GetVenueInstrument(ctx, req.VenueID, req.VenueSymbol)
	if existing != nil {
		return nil, domain.NewError(domain.ErrCodeConfigInvalid, fmt.Sprintf("venue symbol %s already exists for this venue", req.VenueSymbol))
	}

	vi := &VenueInstrument{
		ID:            uuid.New(),
		VenueID:       req.VenueID,
		InstrumentID:  req.InstrumentID,
		VenueSymbol:   req.VenueSymbol,
		Status:        "ACTIVE",
		VenueMetadata: []byte(`{}`),
	}

	if err := s.repo.CreateVenueInstrument(ctx, vi); err != nil {
		return nil, domain.WrapError(domain.ErrCodeConfigInvalid, "failed to create venue instrument", err)
	}

	return vi, nil
}

func (s *Service) ListVenueInstruments(ctx context.Context, venueID uuid.UUID) ([]*VenueInstrument, error) {
	return s.repo.ListVenueInstruments(ctx, venueID)
}
