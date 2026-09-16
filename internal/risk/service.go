package risk

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Service struct {
	repo   *Repository
	engine *Engine
}

func NewService(repo *Repository, config RiskConfig) *Service {
	return &Service{
		repo:   repo,
		engine: NewEngine(config),
	}
}

func (s *Service) CreatePolicy(ctx context.Context, req *CreatePolicyRequest, tenantID uuid.UUID) (*RiskPolicy, error) {
	policy := &RiskPolicy{
		ID:         uuid.New(),
		TenantID:   tenantID,
		Name:       req.Name,
		PolicyType: req.PolicyType,
		Config:     req.Config,
		Status:     PolicyStatusActive,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	if err := s.repo.CreatePolicy(ctx, policy); err != nil {
		return nil, fmt.Errorf("create policy: %w", err)
	}

	s.engine.AddPolicy(policy)
	return policy, nil
}

func (s *Service) GetPolicy(ctx context.Context, id uuid.UUID) (*RiskPolicy, error) {
	return s.repo.GetPolicyByID(ctx, id)
}

func (s *Service) ListPolicies(ctx context.Context, tenantID uuid.UUID) ([]*RiskPolicy, error) {
	return s.repo.ListPolicies(ctx, tenantID)
}

func (s *Service) UpdatePolicy(ctx context.Context, id uuid.UUID, req *UpdatePolicyRequest) (*RiskPolicy, error) {
	policy, err := s.repo.GetPolicyByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if req.Name != nil {
		policy.Name = *req.Name
	}
	if req.Status != nil {
		policy.Status = *req.Status
	}
	if req.Config != nil {
		policy.Config = *req.Config
	}
	policy.UpdatedAt = time.Now()

	if err := s.repo.UpdatePolicy(ctx, policy); err != nil {
		return nil, fmt.Errorf("update policy: %w", err)
	}

	return policy, nil
}

func (s *Service) DeletePolicy(ctx context.Context, id uuid.UUID) error {
	s.engine.RemovePolicy(id)
	return s.repo.DeletePolicy(ctx, id)
}

func (s *Service) EnableKillSwitch(reason string) {
	s.engine.EnableKillSwitch(reason)
}

func (s *Service) DisableKillSwitch() {
	s.engine.DisableKillSwitch()
}

func (s *Service) IsKillSwitchActive() bool {
	return s.engine.IsKillSwitchActive()
}

func (s *Service) GetKillSwitch() KillSwitch {
	return s.engine.GetKillSwitch()
}

func (s *Service) PreTradeCheck(ctx context.Context, req *PreTradeCheckRequest, balance, currentPosition, currentLeverage, marketDepth float64, marketHealthy, venueHealthy bool) *PreTradeCheckResult {
	return s.engine.EvaluatePreTrade(req, balance, currentPosition, currentLeverage, marketDepth, marketHealthy, venueHealthy)
}

func (s *Service) RecordCheck(ctx context.Context, check *RiskCheck) error {
	return s.repo.CreateCheck(ctx, check)
}

func (s *Service) ListChecksByDecision(ctx context.Context, decisionID uuid.UUID) ([]*RiskCheck, error) {
	return s.repo.ListChecksByDecision(ctx, decisionID)
}
