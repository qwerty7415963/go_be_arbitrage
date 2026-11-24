package tradergroup

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

// ListAlerts returns all alerts configured for an owned group, newest first,
// each carrying its unread event count.
func (r *Repository) ListAlerts(ctx context.Context, groupID, userID uuid.UUID) ([]*GroupAlert, error) {
	if err := r.requireOwner(ctx, groupID, userID); err != nil {
		return nil, err
	}

	rows, err := r.pool.Query(ctx, `
		SELECT a.id, a.group_id, a.user_id, a.name, a.type, a.params, a.enabled,
			a.cooldown_seconds, a.notify_in_app, a.notify_telegram, a.last_condition_met,
			a.last_value, a.last_triggered_at, a.last_evaluated_at, a.created_at, a.updated_at,
			COALESCE((SELECT COUNT(*)::int FROM group_alert_events e
				WHERE e.alert_id = a.id AND e.read_at IS NULL), 0) AS unread_events
		FROM group_alerts a
		WHERE a.group_id = $1
		ORDER BY a.created_at DESC`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*GroupAlert, 0)
	for rows.Next() {
		var a GroupAlert
		var paramsBytes []byte
		if err := rows.Scan(&a.ID, &a.GroupID, &a.UserID, &a.Name, &a.Type, &paramsBytes,
			&a.Enabled, &a.CooldownSeconds, &a.NotifyInApp, &a.NotifyTelegram,
			&a.LastConditionMet, &a.LastValue, &a.LastTriggeredAt, &a.LastEvaluatedAt,
			&a.CreatedAt, &a.UpdatedAt, &a.UnreadEvents); err != nil {
			return nil, err
		}
		if len(paramsBytes) > 0 {
			_ = json.Unmarshal(paramsBytes, &a.Params)
		}
		if a.Params == nil {
			a.Params = map[string]any{}
		}
		tmp := a
		out = append(out, &tmp)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// ListAlertEvents returns a page of alert events for an owned group.
func (r *Repository) ListAlertEvents(ctx context.Context, groupID, userID uuid.UUID, unread bool, limit int, cursor string) (*AlertEventPage, error) {
	if err := r.requireOwner(ctx, groupID, userID); err != nil {
		return nil, err
	}

	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if limit < 1 {
		limit = 1
	}

	// unread count for the whole group (independent of filters).
	var unreadCount int
	if err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*)::int FROM group_alert_events WHERE group_id = $1 AND read_at IS NULL`,
		groupID).Scan(&unreadCount); err != nil {
		return nil, err
	}

	query := `SELECT id, alert_id, group_id, type, payload, triggered_at, read_at
		FROM group_alert_events
		WHERE group_id = $1`
	args := []any{groupID}
	argN := 2

	if unread {
		query += fmt.Sprintf(" AND read_at IS NULL")
	}

	if strings.TrimSpace(cursor) != "" {
		occ, id, err := decodeAlertEventCursor(r.cursorSecret, cursor)
		if err != nil {
			return nil, err
		}
		query += fmt.Sprintf(" AND (triggered_at < $%d OR (triggered_at = $%d AND id < $%d))", argN, argN, argN+1)
		args = append(args, occ, id)
		argN += 2
	}

	query += fmt.Sprintf(" ORDER BY triggered_at DESC, id DESC LIMIT $%d", argN)
	args = append(args, limit+1)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]AlertEvent, 0, limit)
	for rows.Next() {
		var ev AlertEvent
		var payloadBytes []byte
		if err := rows.Scan(&ev.ID, &ev.AlertID, &ev.GroupID, &ev.Type, &payloadBytes, &ev.TriggeredAt, &ev.ReadAt); err != nil {
			return nil, err
		}
		if len(payloadBytes) > 0 {
			_ = json.Unmarshal(payloadBytes, &ev.Payload)
		}
		if ev.Payload == nil {
			ev.Payload = map[string]any{}
		}
		items = append(items, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}

	page := &AlertEventPage{
		Items:       items,
		HasMore:     hasMore,
		UnreadCount: unreadCount,
	}
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		cur, err := encodeAlertEventCursor(r.cursorSecret, last.TriggeredAt, last.ID)
		if err != nil {
			return nil, err
		}
		page.NextCursor = cur
	}
	return page, nil
}

// Exported aliases / alternate names for HMAC helpers (compatibility).
func EncodeAlertEventCursor(secret []byte, triggeredAt time.Time, id uuid.UUID) (string, error) {
	return encodeAlertEventCursor(secret, triggeredAt, id)
}
func DecodeAlertEventCursor(secret []byte, raw string) (time.Time, uuid.UUID, error) {
	return decodeAlertEventCursor(secret, raw)
}
func EncodeAlertCursor(secret []byte, triggeredAt time.Time, id uuid.UUID) (string, error) {
	return encodeAlertEventCursor(secret, triggeredAt, id)
}
func DecodeAlertCursor(secret []byte, raw string) (time.Time, uuid.UUID, error) {
	return decodeAlertEventCursor(secret, raw)
}
func encodeAlertCursor(secret []byte, triggeredAt time.Time, id uuid.UUID) (string, error) {
	return encodeAlertEventCursor(secret, triggeredAt, id)
}
func decodeAlertCursor(secret []byte, raw string) (time.Time, uuid.UUID, error) {
	return decodeAlertEventCursor(secret, raw)
}

// encodeAlertEventCursor seals triggered_at + id.
func encodeAlertEventCursor(secret []byte, triggeredAt time.Time, id uuid.UUID) (string, error) {
	payload, err := json.Marshal(map[string]any{
		"o": triggeredAt.UTC().Format(time.RFC3339Nano),
		"i": id.String(),
	})
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

// decodeAlertEventCursor verifies and opens an alert event cursor.
func decodeAlertEventCursor(secret []byte, raw string) (time.Time, uuid.UUID, error) {
	parts := strings.Split(strings.TrimSpace(raw), ".")
	if len(parts) != 2 {
		return time.Time{}, uuid.Nil, domain.NewError(domain.ErrCodeInvalidFilter, "malformed cursor")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return time.Time{}, uuid.Nil, domain.NewError(domain.ErrCodeInvalidFilter, "malformed cursor")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}, uuid.Nil, domain.NewError(domain.ErrCodeInvalidFilter, "malformed cursor")
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(payload)
	if !hmac.Equal(mac.Sum(nil), sig) {
		return time.Time{}, uuid.Nil, domain.NewError(domain.ErrCodeInvalidFilter, "invalid cursor signature")
	}
	var c struct {
		O string `json:"o"`
		I string `json:"i"`
	}
	if err := json.Unmarshal(payload, &c); err != nil {
		return time.Time{}, uuid.Nil, domain.NewError(domain.ErrCodeInvalidFilter, "malformed cursor")
	}
	occ, err := time.Parse(time.RFC3339Nano, c.O)
	if err != nil {
		return time.Time{}, uuid.Nil, domain.NewError(domain.ErrCodeInvalidFilter, "malformed cursor")
	}
	id, err := uuid.Parse(c.I)
	if err != nil {
		return time.Time{}, uuid.Nil, domain.NewError(domain.ErrCodeInvalidFilter, "malformed cursor")
	}
	return occ.UTC(), id, nil
}
