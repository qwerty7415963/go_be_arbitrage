package reconciliation

import (
	"encoding/json"
	"fmt"
	"math"
)

type Comparator struct{}

func NewComparator() *Comparator {
	return &Comparator{}
}

func (c *Comparator) CompareBalance(internal, external *BalanceState) *ReconciliationItem {
	if internal == nil && external == nil {
		return c.newItem(EntityBalance, ResultMatch, nil, nil, nil)
	}
	if internal == nil {
		return c.newItem(EntityBalance, ResultMissingInternal, nil, external, nil)
	}
	if external == nil {
		return c.newItem(EntityBalance, ResultMissingExternal, internal, nil, nil)
	}

	if math.Abs(internal.Amount-external.Amount) < 0.0001 &&
		math.Abs(internal.Reserved-external.Reserved) < 0.0001 {
		return c.newItem(EntityBalance, ResultMatch, internal, external, nil)
	}

	diff := map[string]interface{}{
		"amount_diff":     internal.Amount - external.Amount,
		"reserved_diff":   internal.Reserved - external.Reserved,
		"internal_amount": internal.Amount,
		"external_amount": external.Amount,
	}
	return c.newItem(EntityBalance, ResultMismatch, internal, external, diff)
}

func (c *Comparator) ComparePosition(internal, external *PositionState) *ReconciliationItem {
	if internal == nil && external == nil {
		return c.newItem(EntityPosition, ResultMatch, nil, nil, nil)
	}
	if internal == nil {
		return c.newItem(EntityPosition, ResultMissingInternal, nil, external, nil)
	}
	if external == nil {
		return c.newItem(EntityPosition, ResultMissingExternal, internal, nil, nil)
	}

	if internal.Side == external.Side &&
		math.Abs(internal.Quantity-external.Quantity) < 0.0001 &&
		math.Abs(internal.AvgPrice-external.AvgPrice) < 0.0001 {
		return c.newItem(EntityPosition, ResultMatch, internal, external, nil)
	}

	diff := map[string]interface{}{
		"side_diff":      internal.Side != external.Side,
		"quantity_diff":  internal.Quantity - external.Quantity,
		"avg_price_diff": internal.AvgPrice - external.AvgPrice,
	}
	return c.newItem(EntityPosition, ResultMismatch, internal, external, diff)
}

func (c *Comparator) CompareOrder(internal, external *OrderState) *ReconciliationItem {
	if internal == nil && external == nil {
		return c.newItem(EntityOrder, ResultMatch, nil, nil, nil)
	}
	if internal == nil {
		return c.newItem(EntityOrder, ResultMissingInternal, nil, external, nil)
	}
	if external == nil {
		return c.newItem(EntityOrder, ResultMissingExternal, internal, nil, nil)
	}

	if internal.Status == external.Status &&
		math.Abs(internal.Quantity-external.Quantity) < 0.0001 &&
		math.Abs(internal.FilledQty-external.FilledQty) < 0.0001 {
		return c.newItem(EntityOrder, ResultMatch, internal, external, nil)
	}

	diff := map[string]interface{}{
		"status_diff":     internal.Status != external.Status,
		"quantity_diff":   internal.Quantity - external.Quantity,
		"filled_qty_diff": internal.FilledQty - external.FilledQty,
	}
	return c.newItem(EntityOrder, ResultMismatch, internal, external, diff)
}

func (c *Comparator) CompareMargin(internal, external *MarginState) *ReconciliationItem {
	if internal == nil && external == nil {
		return c.newItem(EntityMargin, ResultMatch, nil, nil, nil)
	}
	if internal == nil {
		return c.newItem(EntityMargin, ResultMissingInternal, nil, external, nil)
	}
	if external == nil {
		return c.newItem(EntityMargin, ResultMissingExternal, internal, nil, nil)
	}

	if math.Abs(internal.UsedMargin-external.UsedMargin) < 0.01 &&
		math.Abs(internal.AvailableMargin-external.AvailableMargin) < 0.01 &&
		math.Abs(internal.MarginRatio-external.MarginRatio) < 0.001 {
		return c.newItem(EntityMargin, ResultMatch, internal, external, nil)
	}

	diff := map[string]interface{}{
		"margin_ratio_diff": internal.MarginRatio - external.MarginRatio,
		"used_margin_diff":  internal.UsedMargin - external.UsedMargin,
		"available_diff":    internal.AvailableMargin - external.AvailableMargin,
	}
	return c.newItem(EntityMargin, ResultMismatch, internal, external, diff)
}

func (c *Comparator) CompareFills(internal, external []*FillState) *ReconciliationItem {
	if len(internal) == 0 && len(external) == 0 {
		return c.newItem(EntityFill, ResultMatch, nil, nil, nil)
	}
	if len(internal) == 0 {
		return c.newItem(EntityFill, ResultMissingInternal, nil, map[string]int{"count": len(external)}, nil)
	}
	if len(external) == 0 {
		return c.newItem(EntityFill, ResultMissingExternal, map[string]int{"count": len(internal)}, nil, nil)
	}

	extMap := make(map[string]*FillState)
	for _, f := range external {
		extMap[f.FillID] = f
	}

	allMatch := true
	for _, intFill := range internal {
		extFill, ok := extMap[intFill.FillID]
		if !ok {
			allMatch = false
			break
		}
		if math.Abs(intFill.Quantity-extFill.Quantity) > 0.0001 ||
			math.Abs(intFill.Price-extFill.Price) > 0.0001 {
			allMatch = false
			break
		}
	}

	if allMatch {
		return c.newItem(EntityFill, ResultMatch, internal, external, nil)
	}

	diff := map[string]interface{}{
		"internal_count": len(internal),
		"external_count": len(external),
	}
	return c.newItem(EntityFill, ResultMismatch, internal, external, diff)
}

type FillState struct {
	FillID   string  `json:"fill_id"`
	OrderID  string  `json:"order_id"`
	Quantity float64 `json:"quantity"`
	Price    float64 `json:"price"`
}

func (c *Comparator) newItem(entityType EntityType, result ItemResult, internal, external, diff interface{}) *ReconciliationItem {
	intJSON, _ := json.Marshal(internal)
	extJSON, _ := json.Marshal(external)
	diffJSON, _ := json.Marshal(diff)

	return &ReconciliationItem{
		EntityType:    entityType,
		Result:        result,
		InternalState: intJSON,
		ExternalState: extJSON,
		Diff:          diffJSON,
	}
}

func (c *Comparator) SummarizeItems(items []*ReconciliationItem) *RunSummary {
	summary := &RunSummary{TotalItems: len(items)}
	for _, item := range items {
		switch item.Result {
		case ResultMatch:
			summary.Matched++
		case ResultMismatch:
			summary.Mismatched++
		case ResultMissingInternal:
			summary.MissingInt++
		case ResultMissingExternal:
			summary.MissingExt++
		}
	}
	return summary
}

func (c *Comparator) OverallStatus(summary *RunSummary) RunStatus {
	if summary.Mismatched > 0 || summary.MissingInt > 0 || summary.MissingExt > 0 {
		return RunStatusMismatch
	}
	return RunStatusMatched
}

func (c *Comparator) SummaryToJSON(summary *RunSummary) json.RawMessage {
	b, _ := json.Marshal(summary)
	return b
}

func (c *Comparator) DiffString(a, b float64) string {
	return fmt.Sprintf("%.6f", a-b)
}
