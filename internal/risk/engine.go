package risk

import (
	"time"

	"github.com/google/uuid"
)

type Engine struct {
	calculator *Calculator
	policies   map[uuid.UUID]*RiskPolicy
	killSwitch KillSwitch
}

func NewEngine(config RiskConfig) *Engine {
	return &Engine{
		calculator: NewCalculator(config),
		policies:   make(map[uuid.UUID]*RiskPolicy),
		killSwitch: KillSwitch{Enabled: false},
	}
}

func (e *Engine) AddPolicy(policy *RiskPolicy) {
	e.policies[policy.ID] = policy
}

func (e *Engine) RemovePolicy(id uuid.UUID) {
	delete(e.policies, id)
}

func (e *Engine) GetPolicies() []*RiskPolicy {
	var policies []*RiskPolicy
	for _, p := range e.policies {
		policies = append(policies, p)
	}
	return policies
}

func (e *Engine) EnableKillSwitch(reason string) {
	now := time.Now()
	e.killSwitch = KillSwitch{
		Enabled:   true,
		EnabledAt: &now,
		Reason:    reason,
	}
}

func (e *Engine) DisableKillSwitch() {
	e.killSwitch = KillSwitch{Enabled: false}
}

func (e *Engine) IsKillSwitchActive() bool {
	return e.killSwitch.Enabled
}

func (e *Engine) GetKillSwitch() KillSwitch {
	return e.killSwitch
}

func (e *Engine) EvaluatePreTrade(req *PreTradeCheckRequest, balance, currentPosition, currentLeverage, marketDepth float64, marketHealthy, venueHealthy bool) *PreTradeCheckResult {
	checks := []*RiskCheck{}
	reasons := []string{}

	// Kill switch check
	ksCheck := e.calculator.KillSwitchCheck(e.killSwitch.Enabled)
	checks = append(checks, ksCheck)
	if ksCheck.Result == CheckResultFail {
		reasons = append(reasons, "kill switch is active")
	}

	// Balance check
	balCheck := e.calculator.BalanceCheck(balance, req.Notional)
	checks = append(checks, balCheck)
	if balCheck.Result == CheckResultFail {
		reasons = append(reasons, "insufficient balance")
	}

	// Position limit check
	posCheck := e.calculator.PositionLimitCheck(currentPosition, 100000)
	checks = append(checks, posCheck)
	if posCheck.Result == CheckResultFail {
		reasons = append(reasons, "position limit exceeded")
	}

	// Notional limit check
	notionalCheck := e.calculator.NotionalLimitCheck(req.Notional, 50000)
	checks = append(checks, notionalCheck)
	if notionalCheck.Result == CheckResultFail {
		reasons = append(reasons, "notional limit exceeded")
	}

	// Leverage check
	levCheck := e.calculator.LeverageCheck(currentLeverage, 10)
	checks = append(checks, levCheck)
	if levCheck.Result == CheckResultFail {
		reasons = append(reasons, "leverage limit exceeded")
	}

	// Market health check
	mktCheck := e.calculator.MarketHealthCheck(marketHealthy)
	checks = append(checks, mktCheck)
	if mktCheck.Result == CheckResultFail {
		reasons = append(reasons, "market unhealthy")
	}

	// Venue health check
	venueCheck := e.calculator.VenueHealthCheck(venueHealthy)
	checks = append(checks, venueCheck)
	if venueCheck.Result == CheckResultFail {
		reasons = append(reasons, "venue unhealthy")
	}

	// Determine action
	action := RiskActionApprove
	for _, check := range checks {
		if check.Result == CheckResultFail {
			action = RiskActionReject
			break
		}
	}

	return &PreTradeCheckResult{
		Action:  action,
		Checks:  checks,
		Reasons: reasons,
	}
}
