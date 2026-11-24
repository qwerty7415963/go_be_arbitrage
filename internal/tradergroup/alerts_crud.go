package tradergroup

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

var validAlertTypes = map[string]bool{
	"unrealized_pnl_above": true,
	"unrealized_pnl_below": true,
	"realized_pnl_reaches": true,
	"roi_reaches":          true,
	"position_opened":      true,
	"position_closed":      true,
	"position_increased":   true,
	"position_reduced":     true,
	"exposure_above":       true,
	"long_short_shift":     true,
	"wallet_inactive":      true,
	"wallet_stale":         true,
	"wallet_added":         true,
	"wallet_removed":       true,
}

// CreateAlert inserts a new alert for an owned group.
func (r *Repository) CreateAlert(ctx context.Context, groupID, userID uuid.UUID, req CreateAlertRequest) (*GroupAlert, error) {
	if err := r.requireOwner(ctx, groupID, userID); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(req.Name)
	if utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 100 {
		return nil, domain.NewError(domain.ErrCodeValidation, "name must be 1..100 characters")
	}
	if !validAlertTypes[req.Type] {
		return nil, domain.NewError(domain.ErrCodeInvalidAlertType, "invalid alert type")
	}
	params := req.Params
	if params == nil {
		return nil, domain.NewError(domain.ErrCodeInvalidAlertParams, "params must not be nil")
	}
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	cooldown := 300
	if req.CooldownSeconds != nil {
		if *req.CooldownSeconds < 0 || *req.CooldownSeconds > 86400 {
			return nil, domain.NewError(domain.ErrCodeInvalidAlertParams, "cooldown_seconds must be 0..86400")
		}
		cooldown = *req.CooldownSeconds
	}
	notifyInApp := true
	if req.NotifyInApp != nil {
		notifyInApp = *req.NotifyInApp
	}
	notifyTelegram := false
	if req.NotifyTelegram != nil {
		notifyTelegram = *req.NotifyTelegram
	}

	var a GroupAlert
	var paramsBytes []byte
	err = r.pool.QueryRow(ctx, `
		INSERT INTO group_alerts (group_id, user_id, name, type, params, enabled, cooldown_seconds, notify_in_app, notify_telegram)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7, $8, $9)
		RETURNING id, group_id, user_id, name, type, params, enabled, cooldown_seconds,
			notify_in_app, notify_telegram, last_condition_met, last_value,
			last_triggered_at, last_evaluated_at, created_at, updated_at`,
		groupID, userID, name, req.Type, paramsJSON, enabled, cooldown, notifyInApp, notifyTelegram,
	).Scan(&a.ID, &a.GroupID, &a.UserID, &a.Name, &a.Type, &paramsBytes, &a.Enabled,
		&a.CooldownSeconds, &a.NotifyInApp, &a.NotifyTelegram, &a.LastConditionMet,
		&a.LastValue, &a.LastTriggeredAt, &a.LastEvaluatedAt, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, domain.NewError(domain.ErrCodeAlertDuplicate, "alert name already exists")
		}
		return nil, err
	}
	if len(paramsBytes) > 0 {
		_ = json.Unmarshal(paramsBytes, &a.Params)
	}
	if a.Params == nil {
		a.Params = map[string]any{}
	}
	return &a, nil
}

// GetAlert returns one alert owned (via its group) by userID.
func (r *Repository) GetAlert(ctx context.Context, alertID, userID uuid.UUID) (*GroupAlert, error) {
	var owner uuid.UUID
	err := r.pool.QueryRow(ctx, `
		SELECT g.user_id
		FROM group_alerts a
		JOIN trader_groups g ON g.id = a.group_id
		WHERE a.id = $1`, alertID).Scan(&owner)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, domain.NewError(domain.ErrCodeAlertNotFound, "alert not found")
		}
		return nil, err
	}
	if owner != userID {
		return nil, forbidden()
	}

	var a GroupAlert
	var paramsBytes []byte
	err = r.pool.QueryRow(ctx, `
		SELECT id, group_id, user_id, name, type, params, enabled, cooldown_seconds,
			notify_in_app, notify_telegram, last_condition_met, last_value,
			last_triggered_at, last_evaluated_at, created_at, updated_at
		FROM group_alerts
		WHERE id = $1`, alertID,
	).Scan(&a.ID, &a.GroupID, &a.UserID, &a.Name, &a.Type, &paramsBytes, &a.Enabled,
		&a.CooldownSeconds, &a.NotifyInApp, &a.NotifyTelegram, &a.LastConditionMet,
		&a.LastValue, &a.LastTriggeredAt, &a.LastEvaluatedAt, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, domain.NewError(domain.ErrCodeAlertNotFound, "alert not found")
		}
		return nil, err
	}
	if len(paramsBytes) > 0 {
		_ = json.Unmarshal(paramsBytes, &a.Params)
	}
	if a.Params == nil {
		a.Params = map[string]any{}
	}
	_ = r.pool.QueryRow(ctx, `
		SELECT COUNT(*)::int FROM group_alert_events WHERE alert_id = $1 AND read_at IS NULL`,
		alertID).Scan(&a.UnreadEvents)
	return &a, nil
}
