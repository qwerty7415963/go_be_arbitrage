package risk

import (
	"time"

	"github.com/google/uuid"
)

type Calculator struct {
	config RiskConfig
}

func NewCalculator(config RiskConfig) *Calculator {
	return &Calculator{config: config}
}

func (c *Calculator) BalanceCheck(availableBalance, notional float64) *RiskCheck {
	result := CheckResultPass
	if availableBalance < notional {
		result = CheckResultFail
	}
	return &RiskCheck{
		ID:             uuid.New(),
		CheckType:      "BALANCE",
		Result:         result,
		ObservedValue:  availableBalance,
		ThresholdValue: notional,
		CreatedAt:      now(),
	}
}

func (c *Calculator) PositionLimitCheck(currentPosition, maxPosition float64) *RiskCheck {
	result := CheckResultPass
	if currentPosition >= maxPosition {
		result = CheckResultFail
	}
	return &RiskCheck{
		ID:             uuid.New(),
		CheckType:      "POSITION_LIMIT",
		Result:         result,
		ObservedValue:  currentPosition,
		ThresholdValue: maxPosition,
		CreatedAt:      now(),
	}
}

func (c *Calculator) NotionalLimitCheck(tradeNotional, maxNotional float64) *RiskCheck {
	result := CheckResultPass
	if tradeNotional > maxNotional {
		result = CheckResultFail
	}
	return &RiskCheck{
		ID:             uuid.New(),
		CheckType:      "NOTIONAL_LIMIT",
		Result:         result,
		ObservedValue:  tradeNotional,
		ThresholdValue: maxNotional,
		CreatedAt:      now(),
	}
}

func (c *Calculator) LeverageCheck(currentLeverage, maxLeverage float64) *RiskCheck {
	result := CheckResultPass
	if currentLeverage > maxLeverage {
		result = CheckResultFail
	}
	return &RiskCheck{
		ID:             uuid.New(),
		CheckType:      "LEVERAGE",
		Result:         result,
		ObservedValue:  currentLeverage,
		ThresholdValue: maxLeverage,
		CreatedAt:      now(),
	}
}

func (c *Calculator) MarketHealthCheck(isHealthy bool) *RiskCheck {
	result := CheckResultPass
	if !isHealthy {
		result = CheckResultFail
	}
	return &RiskCheck{
		ID:        uuid.New(),
		CheckType: "MARKET_HEALTH",
		Result:    result,
		ObservedValue: map[string]interface{}{
			"healthy": isHealthy,
		},
		CreatedAt: now(),
	}
}

func (c *Calculator) VenueHealthCheck(isHealthy bool) *RiskCheck {
	result := CheckResultPass
	if !isHealthy {
		result = CheckResultFail
	}
	return &RiskCheck{
		ID:        uuid.New(),
		CheckType: "VENUE_HEALTH",
		Result:    result,
		ObservedValue: map[string]interface{}{
			"healthy": isHealthy,
		},
		CreatedAt: now(),
	}
}

func (c *Calculator) ConcentrationCheck(currentConcentration, maxConcentration float64) *RiskCheck {
	result := CheckResultPass
	if currentConcentration > maxConcentration {
		result = CheckResultFail
	}
	return &RiskCheck{
		ID:             uuid.New(),
		CheckType:      "CONCENTRATION",
		Result:         result,
		ObservedValue:  currentConcentration,
		ThresholdValue: maxConcentration,
		CreatedAt:      now(),
	}
}

func (c *Calculator) FundingRateCheck(fundingRate, maxRate float64) *RiskCheck {
	result := CheckResultPass
	if fundingRate > maxRate || fundingRate < -maxRate {
		result = CheckResultFail
	}
	return &RiskCheck{
		ID:             uuid.New(),
		CheckType:      "FUNDING_RATE",
		Result:         result,
		ObservedValue:  fundingRate,
		ThresholdValue: maxRate,
		CreatedAt:      now(),
	}
}

func (c *Calculator) KillSwitchCheck(enabled bool) *RiskCheck {
	result := CheckResultPass
	if enabled {
		result = CheckResultFail
	}
	return &RiskCheck{
		ID:        uuid.New(),
		CheckType: "KILL_SWITCH",
		Result:    result,
		ObservedValue: map[string]interface{}{
			"enabled": enabled,
		},
		CreatedAt: now(),
	}
}

func (c *Calculator) StrategyAllocationCheck(used, limit float64) *RiskCheck {
	result := CheckResultPass
	if used >= limit {
		result = CheckResultFail
	}
	return &RiskCheck{
		ID:             uuid.New(),
		CheckType:      "STRATEGY_ALLOCATION",
		Result:         result,
		ObservedValue:  used,
		ThresholdValue: limit,
		CreatedAt:      now(),
	}
}

func now() time.Time {
	return time.Now()
}
