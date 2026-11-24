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

// UpdateAlert partially updates an alert owned (via its group) by userID.
func (r *Repository) UpdateAlert(ctx context.Context, alertID, userID uuid.UUID, req UpdateAlertRequest) (*GroupAlert, error) {
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

	if req.Name != nil {
		trimmed := strings.TrimSpace(*req.Name)
		if utf8.RuneCountInString(trimmed) < 1 || utf8.RuneCountInString(trimmed) > 100 {
			return nil, domain.NewError(domain.ErrCodeValidation, "name must be 1..100 characters")
		}
		req.Name = &trimmed
	}
	if req.CooldownSeconds != nil {
		if *req.CooldownSeconds < 0 || *req.CooldownSeconds > 86400 {
			return nil, domain.NewError(domain.ErrCodeInvalidAlertParams, "cooldown_seconds must be 0..86400")
		}
	}

	var namePtr *string
	if req.Name != nil {
		namePtr = req.Name
	}

	var paramsJSON []byte
	if req.Params != nil {
		b, err := json.Marshal(req.Params)
		if err != nil {
			return nil, err
		}
		paramsJSON = b
	}

	var a GroupAlert
	var paramsBytes []byte
	err = r.pool.QueryRow(ctx, `
		UPDATE group_alerts SET
			name = COALESCE($2, name),
			params = COALESCE($3::jsonb, params),
			enabled = COALESCE($4, enabled),
			cooldown_seconds = COALESCE($5, cooldown_seconds),
			notify_in_app = COALESCE($6, notify_in_app),
			notify_telegram = COALESCE($7, notify_telegram),
			updated_at = NOW()
		WHERE id = $1
		RETURNING id, group_id, user_id, name, type, params, enabled, cooldown_seconds,
			notify_in_app, notify_telegram, last_condition_met, last_value,
			last_triggered_at, last_evaluated_at, created_at, updated_at`,
		alertID, namePtr, paramsJSON, req.Enabled, req.CooldownSeconds, req.NotifyInApp, req.NotifyTelegram,
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
	_ = r.pool.QueryRow(ctx, `
		SELECT COUNT(*)::int FROM group_alert_events WHERE alert_id = $1 AND read_at IS NULL`,
		alertID).Scan(&a.UnreadEvents)
	return &a, nil
}

// DeleteAlert removes an alert owned (via its group) by userID; events cascade.
func (r *Repository) DeleteAlert(ctx context.Context, alertID, userID uuid.UUID) error {
	var owner uuid.UUID
	err := r.pool.QueryRow(ctx, `
		SELECT g.user_id
		FROM group_alerts a
		JOIN trader_groups g ON g.id = a.group_id
		WHERE a.id = $1`, alertID).Scan(&owner)
	if err != nil {
		if err == pgx.ErrNoRows {
			return domain.NewError(domain.ErrCodeAlertNotFound, "alert not found")
		}
		return err
	}
	if owner != userID {
		return forbidden()
	}
	_, err = r.pool.Exec(ctx, `DELETE FROM group_alerts WHERE id = $1`, alertID)
	return err
}
