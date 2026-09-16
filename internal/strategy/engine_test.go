package strategy

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/opportunity"
)

func TestEngineMatchesStrategy_InstrumentFilter(t *testing.T) {
	engine := NewEngine()

	instrID := uuid.New()
	instance := &StrategyInstance{
		ID: uuid.New(),
		Config: &StrategyConfig{
			Instruments: []uuid.UUID{instrID},
		},
	}

	oppMatch := &opportunity.Opportunity{
		ID:              uuid.New(),
		OpportunityType: opportunity.OpportunityTypePriceArb,
		InstrumentID:    instrID,
	}

	oppNoMatch := &opportunity.Opportunity{
		ID:              uuid.New(),
		OpportunityType: opportunity.OpportunityTypePriceArb,
		InstrumentID:    uuid.New(),
	}

	if !engine.matchesStrategy(instance, oppMatch) {
		t.Error("expected match for instrument in filter")
	}
	if engine.matchesStrategy(instance, oppNoMatch) {
		t.Error("expected no match for instrument not in filter")
	}
}

func TestEngineMatchesStrategy_NoConfig(t *testing.T) {
	engine := NewEngine()

	instance := &StrategyInstance{
		ID: uuid.New(),
	}

	opp := &opportunity.Opportunity{
		ID:              uuid.New(),
		OpportunityType: opportunity.OpportunityTypePriceArb,
	}

	if !engine.matchesStrategy(instance, opp) {
		t.Error("expected match when no config")
	}
}

func TestEngineMakeDecision_PassesThresholds(t *testing.T) {
	engine := NewEngine()

	instance := &StrategyInstance{
		ID: uuid.New(),
		Config: &StrategyConfig{
			MinNetEdgeBPS: 10,
			MinConfidence: 0.5,
		},
	}

	opp := &opportunity.Opportunity{
		ID:                 uuid.New(),
		Status:             opportunity.OpportunityStatusDetected,
		ExpectedNetEdgeBPS: 15,
		Confidence:         0.8,
	}

	decision := engine.makeDecision(instance, opp)
	if decision == nil {
		t.Fatal("expected decision to be made")
	}
	if decision.Action != "ACCEPT" {
		t.Errorf("expected action ACCEPT, got %s", decision.Action)
	}
}

func TestEngineMakeDecision_BelowEdgeThreshold(t *testing.T) {
	engine := NewEngine()

	instance := &StrategyInstance{
		ID: uuid.New(),
		Config: &StrategyConfig{
			MinNetEdgeBPS: 20,
			MinConfidence: 0.5,
		},
	}

	opp := &opportunity.Opportunity{
		ID:                 uuid.New(),
		Status:             opportunity.OpportunityStatusDetected,
		ExpectedNetEdgeBPS: 15,
		Confidence:         0.8,
	}

	decision := engine.makeDecision(instance, opp)
	if decision != nil {
		t.Error("expected no decision when below edge threshold")
	}
}

func TestEngineMakeDecision_BelowConfidenceThreshold(t *testing.T) {
	engine := NewEngine()

	instance := &StrategyInstance{
		ID: uuid.New(),
		Config: &StrategyConfig{
			MinNetEdgeBPS: 10,
			MinConfidence: 0.9,
		},
	}

	opp := &opportunity.Opportunity{
		ID:                 uuid.New(),
		Status:             opportunity.OpportunityStatusDetected,
		ExpectedNetEdgeBPS: 15,
		Confidence:         0.7,
	}

	decision := engine.makeDecision(instance, opp)
	if decision != nil {
		t.Error("expected no decision when below confidence threshold")
	}
}

func TestEngineMakeDecision_NotDetected(t *testing.T) {
	engine := NewEngine()

	instance := &StrategyInstance{
		ID: uuid.New(),
	}

	opp := &opportunity.Opportunity{
		ID:                 uuid.New(),
		Status:             opportunity.OpportunityStatusExpired,
		ExpectedNetEdgeBPS: 15,
		Confidence:         0.8,
	}

	decision := engine.makeDecision(instance, opp)
	if decision != nil {
		t.Error("expected no decision for non-detected opportunity")
	}
}

func TestEngineStartStopInstance(t *testing.T) {
	engine := NewEngine()

	instance := &StrategyInstance{
		ID:   uuid.New(),
		Name: "Test Strategy",
	}

	running := engine.GetRunningInstances()
	if len(running) != 0 {
		t.Error("expected no running instances")
	}

	oppSvc := opportunity.NewService(nil, opportunity.DefaultScannerConfig())
	engine.StartInstance(t.Context(), instance, oppSvc)

	running = engine.GetRunningInstances()
	if len(running) != 1 {
		t.Errorf("expected 1 running instance, got %d", len(running))
	}

	engine.StopInstance(instance.ID)

	running = engine.GetRunningInstances()
	if len(running) != 0 {
		t.Error("expected 0 running instances after stop")
	}
}

func TestEngineStartInstance_NoDuplicate(t *testing.T) {
	engine := NewEngine()

	instance := &StrategyInstance{
		ID:   uuid.New(),
		Name: "Test Strategy",
	}

	oppSvc := opportunity.NewService(nil, opportunity.DefaultScannerConfig())
	engine.StartInstance(t.Context(), instance, oppSvc)
	engine.StartInstance(t.Context(), instance, oppSvc)

	running := engine.GetRunningInstances()
	if len(running) != 1 {
		t.Errorf("expected 1 running instance (no duplicate), got %d", len(running))
	}
}

func TestStrategyInstance_Creation(t *testing.T) {
	now := time.Now()
	instance := &StrategyInstance{
		ID:        uuid.New(),
		Name:      "Test Strategy",
		Mode:      StrategyModePaper,
		Status:    StrategyStatusDraft,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if instance.Name != "Test Strategy" {
		t.Error("expected name to be set")
	}
	if instance.Mode != StrategyModePaper {
		t.Error("expected mode PAPER")
	}
	if instance.Status != StrategyStatusDraft {
		t.Error("expected status DRAFT")
	}
}

func TestStrategyConfig_Defaults(t *testing.T) {
	config := &StrategyConfig{
		MinNetEdgeBPS:      10,
		MinConfidence:      0.5,
		MaxNotionalPerTrade: "100000",
		MaxPositionUSD:     "500000",
		MaxLeverage:        10,
	}

	if config.MinNetEdgeBPS != 10 {
		t.Error("expected MinNetEdgeBPS 10")
	}
	if config.MinConfidence != 0.5 {
		t.Error("expected MinConfidence 0.5")
	}
	if config.MaxLeverage != 10 {
		t.Error("expected MaxLeverage 10")
	}
}
