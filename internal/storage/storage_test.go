package storage

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestOpportunityModel(t *testing.T) {
	opp := &Opportunity{
		ID:              uuid.New(),
		TenantID:        uuid.New(),
		StrategyTypeID:  uuid.New(),
		InstrumentID:    uuid.New(),
		OpportunityType: OpportunityTypePriceArb,
		DetectedAt:      time.Now(),
		ExpiresAt:       time.Now().Add(1 * time.Hour),
		MarketQuality:   MarketQualityGood,
	}

	if opp.OpportunityType != OpportunityTypePriceArb {
		t.Errorf("expected PRICE_ARBITRAGE, got %s", opp.OpportunityType)
	}
	if opp.MarketQuality != MarketQualityGood {
		t.Errorf("expected GOOD, got %s", opp.MarketQuality)
	}
}

func TestOpportunityLegModel(t *testing.T) {
	leg := &OpportunityLeg{
		ID:             uuid.New(),
		OpportunityID:  uuid.New(),
		LegRole:        "BUY",
		VenueAccountID: uuid.New(),
		InstrumentID:   uuid.New(),
	}

	if leg.LegRole != "BUY" {
		t.Errorf("expected BUY, got %s", leg.LegRole)
	}
}

func TestStrategyDecisionModel(t *testing.T) {
	decision := &StrategyDecision{
		ID:                 uuid.New(),
		StrategyInstanceID: uuid.New(),
		Decision:           "ACCEPT",
		ReasonCode:         "EDGE_THRESHOLD_MET",
		DecisionTimestamp:  time.Now(),
	}

	if decision.Decision != "ACCEPT" {
		t.Errorf("expected ACCEPT, got %s", decision.Decision)
	}
}

func TestRiskDecisionModel(t *testing.T) {
	decision := &RiskDecision{
		ID:                 uuid.New(),
		StrategyInstanceID: uuid.New(),
		Decision:           "ALLOW",
		ReasonCode:         "RISK_LIMITS_OK",
		EvaluatedAt:        time.Now(),
	}

	if decision.Decision != "ALLOW" {
		t.Errorf("expected ALLOW, got %s", decision.Decision)
	}
}

func TestAuditEventModel(t *testing.T) {
	tenantID := uuid.New()
	event := &AuditEvent{
		ID:        1,
		TenantID:  &tenantID,
		ActorType: "USER",
		Action:    "LOGIN",
		OccurredAt: time.Now(),
	}

	if event.ActorType != "USER" {
		t.Errorf("expected USER, got %s", event.ActorType)
	}
	if event.Action != "LOGIN" {
		t.Errorf("expected LOGIN, got %s", event.Action)
	}
}

func TestSystemEventModel(t *testing.T) {
	event := &SystemEvent{
		ID:        1,
		EventType: "APPLICATION_START",
		OccurredAt: time.Now(),
	}

	if event.EventType != "APPLICATION_START" {
		t.Errorf("expected APPLICATION_START, got %s", event.EventType)
	}
}

func TestReplayEventModel(t *testing.T) {
	event := &ReplayEvent{
		ID:        1,
		EventType: "TRADE",
		Timestamp: time.Now(),
	}

	if event.EventType != "TRADE" {
		t.Errorf("expected TRADE, got %s", event.EventType)
	}
}

func TestReplaySequenceModel(t *testing.T) {
	sequence := &ReplaySequence{
		Events:     make([]*ReplayEvent, 0),
		TotalCount: 0,
		StartTime:  time.Now().Add(-1 * time.Hour),
		EndTime:    time.Now(),
	}

	if sequence.TotalCount != 0 {
		t.Errorf("expected 0 events, got %d", sequence.TotalCount)
	}
}

func TestRetentionConfig(t *testing.T) {
	config := DefaultRetentionConfig()

	if config.RawMarketEventsMaxAge != 7*24*time.Hour {
		t.Errorf("expected 7 days, got %v", config.RawMarketEventsMaxAge)
	}
	if config.MarketTradesMaxAge != 30*24*time.Hour {
		t.Errorf("expected 30 days, got %v", config.MarketTradesMaxAge)
	}
	if config.FundingRatesMaxAge != 90*24*time.Hour {
		t.Errorf("expected 90 days, got %v", config.FundingRatesMaxAge)
	}
}

func TestOpportunityType(t *testing.T) {
	tests := []struct {
		opportunityType OpportunityType
		expected        string
	}{
		{OpportunityTypePriceArb, "PRICE_ARBITRAGE"},
		{OpportunityTypeFundingArb, "FUNDING_ARBITRAGE"},
		{OpportunityTypeBasisArb, "BASIS_ARBITRAGE"},
	}

	for _, tt := range tests {
		if string(tt.opportunityType) != tt.expected {
			t.Errorf("expected %s, got %s", tt.expected, string(tt.opportunityType))
		}
	}
}

func TestMarketQuality(t *testing.T) {
	tests := []struct {
		quality MarketQuality
		expected string
	}{
		{MarketQualityGood, "GOOD"},
		{MarketQualityDegraded, "DEGRADED"},
		{MarketQualityUnusable, "UNUSABLE"},
	}

	for _, tt := range tests {
		if string(tt.quality) != tt.expected {
			t.Errorf("expected %s, got %s", tt.expected, string(tt.quality))
		}
	}
}

func TestRetentionResult(t *testing.T) {
	result := &RetentionResult{
		RawMarketEvents:    100,
		MarketTrades:       200,
		MarketTickers:      50,
		FundingRates:       30,
		OrderbookSnapshots: 150,
		OrderbookDeltas:    1000,
		Opportunities:      10,
		SystemEvents:       25,
	}

	total := result.RawMarketEvents + result.MarketTrades + result.MarketTickers +
		result.FundingRates + result.OrderbookSnapshots + result.OrderbookDeltas +
		result.Opportunities + result.SystemEvents

	if total != 1565 {
		t.Errorf("expected total 1565, got %d", total)
	}
}
