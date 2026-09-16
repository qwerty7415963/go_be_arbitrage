package risk

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestBalanceCheck_Sufficient(t *testing.T) {
	calc := NewCalculator(DefaultRiskConfig())
	check := calc.BalanceCheck(10000, 5000)
	if check.Result != CheckResultPass {
		t.Errorf("expected PASS, got %s", check.Result)
	}
}

func TestBalanceCheck_Insufficient(t *testing.T) {
	calc := NewCalculator(DefaultRiskConfig())
	check := calc.BalanceCheck(3000, 5000)
	if check.Result != CheckResultFail {
		t.Errorf("expected FAIL, got %s", check.Result)
	}
}

func TestBalanceCheck_Exact(t *testing.T) {
	calc := NewCalculator(DefaultRiskConfig())
	check := calc.BalanceCheck(5000, 5000)
	if check.Result != CheckResultPass {
		t.Errorf("expected PASS for exact balance, got %s", check.Result)
	}
}

func TestPositionLimitCheck_WithinLimit(t *testing.T) {
	calc := NewCalculator(DefaultRiskConfig())
	check := calc.PositionLimitCheck(50000, 100000)
	if check.Result != CheckResultPass {
		t.Errorf("expected PASS, got %s", check.Result)
	}
}

func TestPositionLimitCheck_ExceedsLimit(t *testing.T) {
	calc := NewCalculator(DefaultRiskConfig())
	check := calc.PositionLimitCheck(100000, 100000)
	if check.Result != CheckResultFail {
		t.Errorf("expected FAIL, got %s", check.Result)
	}
}

func TestNotionalLimitCheck_WithinLimit(t *testing.T) {
	calc := NewCalculator(DefaultRiskConfig())
	check := calc.NotionalLimitCheck(30000, 50000)
	if check.Result != CheckResultPass {
		t.Errorf("expected PASS, got %s", check.Result)
	}
}

func TestNotionalLimitCheck_ExceedsLimit(t *testing.T) {
	calc := NewCalculator(DefaultRiskConfig())
	check := calc.NotionalLimitCheck(60000, 50000)
	if check.Result != CheckResultFail {
		t.Errorf("expected FAIL, got %s", check.Result)
	}
}

func TestLeverageCheck_WithinLimit(t *testing.T) {
	calc := NewCalculator(DefaultRiskConfig())
	check := calc.LeverageCheck(5, 10)
	if check.Result != CheckResultPass {
		t.Errorf("expected PASS, got %s", check.Result)
	}
}

func TestLeverageCheck_ExceedsLimit(t *testing.T) {
	calc := NewCalculator(DefaultRiskConfig())
	check := calc.LeverageCheck(15, 10)
	if check.Result != CheckResultFail {
		t.Errorf("expected FAIL, got %s", check.Result)
	}
}

func TestMarketHealthCheck_Healthy(t *testing.T) {
	calc := NewCalculator(DefaultRiskConfig())
	check := calc.MarketHealthCheck(true)
	if check.Result != CheckResultPass {
		t.Errorf("expected PASS, got %s", check.Result)
	}
}

func TestMarketHealthCheck_Unhealthy(t *testing.T) {
	calc := NewCalculator(DefaultRiskConfig())
	check := calc.MarketHealthCheck(false)
	if check.Result != CheckResultFail {
		t.Errorf("expected FAIL, got %s", check.Result)
	}
}

func TestVenueHealthCheck_Healthy(t *testing.T) {
	calc := NewCalculator(DefaultRiskConfig())
	check := calc.VenueHealthCheck(true)
	if check.Result != CheckResultPass {
		t.Errorf("expected PASS, got %s", check.Result)
	}
}

func TestVenueHealthCheck_Unhealthy(t *testing.T) {
	calc := NewCalculator(DefaultRiskConfig())
	check := calc.VenueHealthCheck(false)
	if check.Result != CheckResultFail {
		t.Errorf("expected FAIL, got %s", check.Result)
	}
}

func TestConcentrationCheck_WithinLimit(t *testing.T) {
	calc := NewCalculator(DefaultRiskConfig())
	check := calc.ConcentrationCheck(0.3, 0.5)
	if check.Result != CheckResultPass {
		t.Errorf("expected PASS, got %s", check.Result)
	}
}

func TestConcentrationCheck_ExceedsLimit(t *testing.T) {
	calc := NewCalculator(DefaultRiskConfig())
	check := calc.ConcentrationCheck(0.6, 0.5)
	if check.Result != CheckResultFail {
		t.Errorf("expected FAIL, got %s", check.Result)
	}
}

func TestKillSwitchCheck_Inactive(t *testing.T) {
	calc := NewCalculator(DefaultRiskConfig())
	check := calc.KillSwitchCheck(false)
	if check.Result != CheckResultPass {
		t.Errorf("expected PASS, got %s", check.Result)
	}
}

func TestKillSwitchCheck_Active(t *testing.T) {
	calc := NewCalculator(DefaultRiskConfig())
	check := calc.KillSwitchCheck(true)
	if check.Result != CheckResultFail {
		t.Errorf("expected FAIL, got %s", check.Result)
	}
}

func TestFundingRateCheck_WithinLimit(t *testing.T) {
	calc := NewCalculator(DefaultRiskConfig())
	check := calc.FundingRateCheck(0.5, 1.0)
	if check.Result != CheckResultPass {
		t.Errorf("expected PASS, got %s", check.Result)
	}
}

func TestFundingRateCheck_ExceedsLimit(t *testing.T) {
	calc := NewCalculator(DefaultRiskConfig())
	check := calc.FundingRateCheck(1.5, 1.0)
	if check.Result != CheckResultFail {
		t.Errorf("expected FAIL, got %s", check.Result)
	}
}

func TestStrategyAllocationCheck_WithinLimit(t *testing.T) {
	calc := NewCalculator(DefaultRiskConfig())
	check := calc.StrategyAllocationCheck(50000, 100000)
	if check.Result != CheckResultPass {
		t.Errorf("expected PASS, got %s", check.Result)
	}
}

func TestStrategyAllocationCheck_ExceedsLimit(t *testing.T) {
	calc := NewCalculator(DefaultRiskConfig())
	check := calc.StrategyAllocationCheck(100000, 100000)
	if check.Result != CheckResultFail {
		t.Errorf("expected FAIL, got %s", check.Result)
	}
}

func TestEngine_KillSwitch_EnableDisable(t *testing.T) {
	engine := NewEngine(DefaultRiskConfig())

	if engine.IsKillSwitchActive() {
		t.Error("expected kill switch inactive initially")
	}

	engine.EnableKillSwitch("test reason")
	if !engine.IsKillSwitchActive() {
		t.Error("expected kill switch active after enable")
	}

	ks := engine.GetKillSwitch()
	if ks.Reason != "test reason" {
		t.Errorf("expected reason 'test reason', got %s", ks.Reason)
	}
	if ks.EnabledAt == nil {
		t.Error("expected EnabledAt to be set")
	}

	engine.DisableKillSwitch()
	if engine.IsKillSwitchActive() {
		t.Error("expected kill switch inactive after disable")
	}
}

func TestEngine_PreTradeCheck_AllPass(t *testing.T) {
	engine := NewEngine(DefaultRiskConfig())

	req := &PreTradeCheckRequest{
		TenantID:   uuid.New(),
		StrategyID: uuid.New(),
		Notional:   5000,
	}

	result := engine.EvaluatePreTrade(req, 100000, 0, 1, 50000, true, true)

	if result.Action != RiskActionApprove {
		t.Errorf("expected APPROVE, got %s", result.Action)
	}
	if len(result.Checks) != 7 {
		t.Errorf("expected 7 checks, got %d", len(result.Checks))
	}
}

func TestEngine_PreTradeCheck_KillSwitchBlocks(t *testing.T) {
	engine := NewEngine(DefaultRiskConfig())
	engine.EnableKillSwitch("emergency")

	req := &PreTradeCheckRequest{
		TenantID:   uuid.New(),
		StrategyID: uuid.New(),
		Notional:   5000,
	}

	result := engine.EvaluatePreTrade(req, 100000, 0, 1, 50000, true, true)

	if result.Action != RiskActionReject {
		t.Errorf("expected REJECT, got %s", result.Action)
	}
	if len(result.Reasons) == 0 {
		t.Error("expected reasons for rejection")
	}
}

func TestEngine_PreTradeCheck_BalanceFails(t *testing.T) {
	engine := NewEngine(DefaultRiskConfig())

	req := &PreTradeCheckRequest{
		TenantID:   uuid.New(),
		StrategyID: uuid.New(),
		Notional:   50000,
	}

	result := engine.EvaluatePreTrade(req, 1000, 0, 1, 50000, true, true)

	if result.Action != RiskActionReject {
		t.Errorf("expected REJECT, got %s", result.Action)
	}
}

func TestEngine_AddRemovePolicy(t *testing.T) {
	engine := NewEngine(DefaultRiskConfig())

	policy := &RiskPolicy{
		ID:        uuid.New(),
		Name:      "Test Policy",
		PolicyType: PolicyTypeGlobal,
		Config:    DefaultRiskConfig(),
		Status:    PolicyStatusActive,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	engine.AddPolicy(policy)
	policies := engine.GetPolicies()
	if len(policies) != 1 {
		t.Errorf("expected 1 policy, got %d", len(policies))
	}

	engine.RemovePolicy(policy.ID)
	policies = engine.GetPolicies()
	if len(policies) != 0 {
		t.Errorf("expected 0 policies, got %d", len(policies))
	}
}
