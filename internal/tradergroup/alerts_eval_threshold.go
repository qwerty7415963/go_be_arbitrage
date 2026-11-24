package tradergroup

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/qwerty7415963/go_be_arbitrage/internal/notify"
)

func (r *Repository) WithNotifyRepo(nr *notify.Repository) *Repository {
	r.notifyRepo = nr
	return r
}

func (r *Repository) isThresholdType(t string) bool {
	switch t {
	case "position_opened", "position_closed", "position_increased", "position_reduced",
		"wallet_added", "wallet_removed", "wallet_inactive", "wallet_stale":
		return false
	}
	return true
}

func (r *Repository) EvaluateAll(ctx context.Context) error {
	rows, err := r.pool.Query(ctx, `SELECT id, group_id, user_id, name, type, params, enabled, cooldown_seconds,
		notify_in_app, notify_telegram, last_condition_met, last_value,
		last_triggered_at, last_evaluated_at, created_at, updated_at
		FROM group_alerts WHERE enabled = TRUE`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var alerts []*GroupAlert
	for rows.Next() {
		var a GroupAlert
		var paramsBytes []byte
		if err := rows.Scan(&a.ID, &a.GroupID, &a.UserID, &a.Name, &a.Type, &paramsBytes,
			&a.Enabled, &a.CooldownSeconds, &a.NotifyInApp, &a.NotifyTelegram,
			&a.LastConditionMet, &a.LastValue, &a.LastTriggeredAt, &a.LastEvaluatedAt,
			&a.CreatedAt, &a.UpdatedAt); err != nil {
			return err
		}
		if len(paramsBytes) > 0 {
			_ = json.Unmarshal(paramsBytes, &a.Params)
		}
		if a.Params == nil {
			a.Params = map[string]any{}
		}
		tmp := a
		alerts = append(alerts, &tmp)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	var firstErr error
	for _, a := range alerts {
		if !r.isThresholdType(a.Type) {
			continue
		}
		if err := r.evaluateThreshold(ctx, a); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (r *Repository) evaluateThreshold(ctx context.Context, alert *GroupAlert) error {
	if !r.isThresholdType(alert.Type) {
		return nil
	}
	metric, ok, err := r.computeThresholdMetric(ctx, alert)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	met := ok && thresholdMet(alert.Type, alert.Params, metric)

	triggered := met && !alert.LastConditionMet
	if triggered && alert.CooldownSeconds > 0 && alert.LastTriggeredAt != nil {
		if now.Sub(*alert.LastTriggeredAt) < time.Duration(alert.CooldownSeconds)*time.Second {
			triggered = false
		}
	}
	if triggered {
		dedupKey := fmt.Sprintf("%s|%s|%d", alert.ID, alert.GroupID, now.Unix())
		payload := map[string]any{"alert_name": alert.Name, "type": alert.Type, "metric": metric}
		if th, has := paramFloat(alert.Params, "threshold"); has {
			payload["threshold"] = th
		}
		if th, has := paramFloat(alert.Params, "threshold_usd"); has {
			payload["threshold_usd"] = th
		}
		r.createAlertEvent(ctx, alert, alert.Type, payload, dedupKey)
		if alert.NotifyTelegram {
			_ = r.enqueueTelegram(ctx, alert, fmt.Sprintf("Alert %s triggered (value: %g)", alert.Name, metric), dedupKey)
		}
		t := now
		alert.LastTriggeredAt = &t
	}
	alert.LastConditionMet = met
	if ok {
		v := metric
		alert.LastValue = &v
	}
	t := now
	alert.LastEvaluatedAt = &t
	_, err = r.pool.Exec(ctx, `UPDATE group_alerts SET last_condition_met=$2, last_value=$3,
		last_triggered_at=$4, last_evaluated_at=$5, updated_at=NOW() WHERE id=$1`,
		alert.ID, met, alert.LastValue, alert.LastTriggeredAt, alert.LastEvaluatedAt)
	return err
}

func (r *Repository) computeThresholdMetric(ctx context.Context, alert *GroupAlert) (float64, bool, error) {
	switch alert.Type {
	case "unrealized_pnl_above", "unrealized_pnl_below":
		st, err := r.Stats(ctx, alert.GroupID, alert.UserID, "")
		if err != nil || st.UnrealizedPnL == nil {
			return 0, false, err
		}
		return *st.UnrealizedPnL, true, nil
	case "realized_pnl_reaches":
		st, err := r.Stats(ctx, alert.GroupID, alert.UserID, stringParam(alert.Params, "period", "30D"))
		if err != nil || st.RealizedPnL == nil {
			return 0, false, err
		}
		return *st.RealizedPnL, true, nil
	case "roi_reaches":
		st, err := r.Stats(ctx, alert.GroupID, alert.UserID, stringParam(alert.Params, "period", "30D"))
		if err != nil || st.ROI == nil {
			return 0, false, err
		}
		return *st.ROI, true, nil
	case "exposure_above":
		pos, err := r.Positions(ctx, alert.GroupID, alert.UserID)
		if err != nil {
			return 0, false, err
		}
		switch stringParam(alert.Params, "metric", "gross") {
		case "net":
			return pos.Totals.NetNtl, true, nil
		case "long":
			return pos.Totals.LongNtl, true, nil
		case "short":
			return pos.Totals.ShortNtl, true, nil
		default:
			return pos.Totals.GrossNtl, true, nil
		}
	case "long_short_shift":
		pos, err := r.Positions(ctx, alert.GroupID, alert.UserID)
		if err != nil || pos.Totals.GrossNtl == 0 {
			return 0, false, err
		}
		return (pos.Totals.NetNtl / pos.Totals.GrossNtl) * 100, true, nil
	default:
		return 0, false, nil
	}
}

func (r *Repository) createAlertEvent(ctx context.Context, alert *GroupAlert, eventType string, payload map[string]any, dedupKey string) bool {
	if !alert.NotifyInApp {
		return false
	}
	b, _ := json.Marshal(payload)
	if payload == nil {
		b = []byte("{}")
	}
	tag, err := r.pool.Exec(ctx, `INSERT INTO group_alert_events (alert_id, group_id, type, payload, dedup_key, triggered_at)
		VALUES ($1, $2, $3, $4::jsonb, $5, NOW()) ON CONFLICT (dedup_key) DO NOTHING`,
		alert.ID, alert.GroupID, eventType, b, dedupKey)
	if err != nil {
		return false
	}
	return tag.RowsAffected() > 0
}

func (r *Repository) enqueueTelegram(ctx context.Context, alert *GroupAlert, body string, dedupKey string) error {
	if r.notifyRepo == nil || !alert.NotifyTelegram {
		return nil
	}
	var notifyEnabled bool
	if err := r.pool.QueryRow(ctx, `SELECT notify_enabled FROM trader_groups WHERE id=$1`, alert.GroupID).Scan(&notifyEnabled); err != nil || !notifyEnabled {
		return nil
	}
	var chatID int64
	if err := r.pool.QueryRow(ctx, `SELECT chat_id FROM telegram_bindings WHERE user_id=$1 AND status='active' LIMIT 1`, alert.UserID).Scan(&chatID); err != nil {
		return nil
	}
	dk := dedupKey
	row := &notify.OutboxRow{UserID: alert.UserID, ChatID: chatID, DedupKey: &dk, Body: body,
		Attempts: 0, NextAttemptAt: time.Now()}
	_, err := r.notifyRepo.InsertOutbox(ctx, row)
	return err
}

func thresholdMet(typ string, params map[string]any, metric float64) bool {
	switch typ {
	case "unrealized_pnl_above", "exposure_above":
		th, _ := paramFloat(params, "threshold_usd")
		if _, ok := paramFloat(params, "threshold_usd"); !ok {
			th, _ = paramFloat(params, "threshold")
		}
		return metric > th
	case "unrealized_pnl_below":
		th, _ := paramFloat(params, "threshold_usd")
		if _, ok := paramFloat(params, "threshold_usd"); !ok {
			th, _ = paramFloat(params, "threshold")
		}
		return metric < th
	case "realized_pnl_reaches", "roi_reaches":
		th, _ := paramFloat(params, "threshold")
		if stringParam(params, "direction", "above") == "below" {
			return metric <= th
		}
		return metric >= th
	case "long_short_shift":
		th, _ := paramFloat(params, "threshold_pct")
		if _, ok := paramFloat(params, "threshold_pct"); !ok {
			th, _ = paramFloat(params, "threshold")
		}
		if th < 0 {
			th = -th
		}
		v := metric
		if v < 0 {
			v = -v
		}
		return v >= th
	default:
		return false
	}
}

func paramFloat(params map[string]any, key string) (float64, bool) {
	if params == nil {
		return 0, false
	}
	v, ok := params[key]
	if !ok || v == nil {
		return 0, false
	}
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	case json.Number:
		f, err := x.Float64()
		if err != nil {
			return 0, false
		}
		return f, true
	}
	return 0, false
}

func stringParam(params map[string]any, key, def string) string {
	if params == nil {
		return def
	}
	v, ok := params[key]
	if !ok || v == nil {
		return def
	}
	if s, ok := v.(string); ok {
		return s
	}
	return def
}
