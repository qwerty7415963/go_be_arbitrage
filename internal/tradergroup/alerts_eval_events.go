package tradergroup

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type tgMember struct {
	venueID uuid.UUID
	addr    string
	alias   string
}

func (r *Repository) EvaluateEventAlerts(ctx context.Context) error {
	rows, err := r.pool.Query(ctx, `SELECT id, group_id, user_id, name, type, params, enabled, cooldown_seconds,
		notify_in_app, notify_telegram, last_condition_met, last_value,
		last_triggered_at, last_evaluated_at, created_at, updated_at
		FROM group_alerts WHERE enabled = TRUE`)
	if err != nil {
		return nil
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
			continue
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
	rows.Close()
	for _, a := range alerts {
		if r.isThresholdType(a.Type) {
			continue
		}
		_ = r.evaluateEvents(ctx, a)
	}
	return nil
}

func (r *Repository) evaluateEvents(ctx context.Context, alert *GroupAlert) error {
	now := time.Now().UTC()
	if alert.LastEvaluatedAt == nil {
		ts := time.Now().UTC()
		_, err := r.pool.Exec(ctx, `UPDATE group_alerts SET last_evaluated_at=$2, updated_at=NOW() WHERE id=$1`, alert.ID, ts)
		if err != nil {
			return err
		}
		alert.LastEvaluatedAt = &ts
		return nil
	}
	lastEval := time.Time{}
	if alert.LastEvaluatedAt != nil {
		lastEval = *alert.LastEvaluatedAt
	}

	members, err := r.groupMembers(ctx, alert.GroupID)
	if err != nil {
		return err
	}

	triggeredAny := false
	coinFilter := stringParam(alert.Params, "coin", "")

	switch alert.Type {
	case "position_opened", "position_increased", "position_reduced":
		if len(members) == 0 {
			break
		}
		vids := make([]uuid.UUID, len(members))
		addrs := make([]string, len(members))
		for i, m := range members {
			vids[i] = m.venueID
			addrs[i] = m.addr
		}
		rows, err := r.pool.Query(ctx, `SELECT id, venue_id, wallet_address, coin, occurred_at
			FROM trader_wallet_event
			WHERE event_type = $1 AND occurred_at > $2
			  AND (venue_id, wallet_address) IN (SELECT unnest($3::uuid[]), unnest($4::text[]))`,
			alert.Type, lastEval, vids, addrs)
		if err != nil {
			return err
		}
		type evRow struct {
			id       int64
			venueID  uuid.UUID
			addr     string
			coin     string
			occurred time.Time
		}
		var evs []evRow
		for rows.Next() {
			var e evRow
			if err := rows.Scan(&e.id, &e.venueID, &e.addr, &e.coin, &e.occurred); err != nil {
				rows.Close()
				return err
			}
			evs = append(evs, e)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, e := range evs {
			if coinFilter != "" && !strings.EqualFold(e.coin, coinFilter) {
				continue
			}
			subject := strings.ToLower(strings.TrimSpace(e.addr))
			if !r.subjectCooldownOK(ctx, alert, subject, now) {
				continue
			}
			payload := map[string]any{
				"wallet_address": subject,
				"venue_id":       e.venueID.String(),
				"coin":           e.coin,
				"event_type":     alert.Type,
			}
			dedupKey := fmt.Sprintf("%s|%s|%d", alert.ID, subject, e.id)
			r.createAlertEvent(ctx, alert, alert.Type, payload, dedupKey)
			if alert.NotifyTelegram {
				_ = r.enqueueTelegram(ctx, alert, fmt.Sprintf("Alert %s: %s %s — %s", alert.Name, alert.Type, e.coin, aliasFor(members, subject)), dedupKey)
			}
			triggeredAny = true
		}

	case "position_closed":
		if len(members) == 0 {
			break
		}
		vids := make([]uuid.UUID, len(members))
		addrs := make([]string, len(members))
		for i, m := range members {
			vids[i] = m.venueID
			addrs[i] = m.addr
		}
		rows, err := r.pool.Query(ctx, `SELECT venue_id, wallet_address, market, opened_at, closed_at
			FROM trader_trades
			WHERE closed_at > $1
			  AND (venue_id, wallet_address) IN (SELECT unnest($2::uuid[]), unnest($3::text[]))`,
			lastEval, vids, addrs)
		if err != nil {
			return err
		}
		type tRow struct {
			venueID  uuid.UUID
			addr     string
			market   string
			openedAt time.Time
			closedAt time.Time
		}
		var trs []tRow
		for rows.Next() {
			var t tRow
			if err := rows.Scan(&t.venueID, &t.addr, &t.market, &t.openedAt, &t.closedAt); err != nil {
				rows.Close()
				return err
			}
			trs = append(trs, t)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, t := range trs {
			if coinFilter != "" && !strings.EqualFold(t.market, coinFilter) {
				continue
			}
			subject := strings.ToLower(strings.TrimSpace(t.addr))
			if !r.subjectCooldownOK(ctx, alert, subject, now) {
				continue
			}
			eventID := fmt.Sprintf("%s|%s|%s|%s", t.venueID.String(), subject, t.market, t.openedAt.UTC().Format(time.RFC3339Nano))
			// include closed_at to make unique
			eventID = eventID + "|" + t.closedAt.UTC().Format(time.RFC3339Nano)
			payload := map[string]any{
				"wallet_address": subject,
				"venue_id":       t.venueID.String(),
				"coin":           t.market,
				"event_type":     alert.Type,
			}
			dedupKey := fmt.Sprintf("%s|%s|%s", alert.ID, subject, eventID)
			r.createAlertEvent(ctx, alert, alert.Type, payload, dedupKey)
			if alert.NotifyTelegram {
				_ = r.enqueueTelegram(ctx, alert, fmt.Sprintf("Alert %s: %s %s — %s", alert.Name, alert.Type, t.market, aliasFor(members, subject)), dedupKey)
			}
			triggeredAny = true
		}

	case "wallet_added", "wallet_removed":
		if len(members) == 0 {
			// still group-level events; no member filter needed but keep
		}
		evType := "added"
		if alert.Type == "wallet_removed" {
			evType = "removed"
		}
		rows, err := r.pool.Query(ctx, `SELECT id, venue_id, wallet_address, created_at
			FROM group_membership_events
			WHERE group_id = $1 AND event_type = $2 AND created_at > $3`,
			alert.GroupID, evType, lastEval)
		if err != nil {
			return err
		}
		type mRow struct {
			id      int64
			venueID uuid.UUID
			addr    string
			created time.Time
		}
		var mrs []mRow
		for rows.Next() {
			var m mRow
			if err := rows.Scan(&m.id, &m.venueID, &m.addr, &m.created); err != nil {
				rows.Close()
				return err
			}
			mrs = append(mrs, m)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, m := range mrs {
			subject := strings.ToLower(strings.TrimSpace(m.addr))
			if !r.subjectCooldownOK(ctx, alert, subject, now) {
				continue
			}
			payload := map[string]any{
				"wallet_address": subject,
				"venue_id":       m.venueID.String(),
				"event_type":     alert.Type,
			}
			dedupKey := fmt.Sprintf("%s|%s|%d", alert.ID, subject, m.id)
			r.createAlertEvent(ctx, alert, alert.Type, payload, dedupKey)
			if alert.NotifyTelegram {
				_ = r.enqueueTelegram(ctx, alert, fmt.Sprintf("Alert %s: %s — %s", alert.Name, alert.Type, aliasFor(members, subject)), dedupKey)
			}
			triggeredAny = true
		}

	case "wallet_inactive":
		days := 7
		if v, ok := paramFloat(alert.Params, "days"); ok && v > 0 {
			days = int(v)
		}
		for _, m := range members {
			subject := strings.ToLower(strings.TrimSpace(m.addr))
			var lastTrade *time.Time
			_ = r.pool.QueryRow(ctx, `SELECT MAX(last_trade_at) FROM trader_period_metrics WHERE venue_id=$1 AND wallet_address=$2`,
				m.venueID, subject).Scan(&lastTrade)
			inactive := lastTrade == nil || now.Sub(*lastTrade) > time.Duration(days)*24*time.Hour
			if !inactive {
				continue
			}
			if !r.subjectCooldownOK(ctx, alert, subject, now) {
				continue
			}
			payload := map[string]any{
				"wallet_address": subject,
				"venue_id":       m.venueID.String(),
				"event_type":     alert.Type,
				"days":           days,
			}
			dedupKey := fmt.Sprintf("%s|%s|%d", alert.ID, subject, now.Unix())
			r.createAlertEvent(ctx, alert, alert.Type, payload, dedupKey)
			if alert.NotifyTelegram {
				_ = r.enqueueTelegram(ctx, alert, fmt.Sprintf("Alert %s: wallet inactive — %s", alert.Name, aliasFor(members, subject)), dedupKey)
			}
			triggeredAny = true
		}

	case "wallet_stale":
		minutes := 60
		if v, ok := paramFloat(alert.Params, "minutes"); ok && v > 0 {
			minutes = int(v)
		}
		for _, m := range members {
			subject := strings.ToLower(strings.TrimSpace(m.addr))
			var status string
			var lastSync *time.Time
			err := r.pool.QueryRow(ctx, `SELECT COALESCE(sync_status,''), last_positions_sync_at FROM trader_sync_state WHERE venue_id=$1 AND wallet_address=$2`,
				m.venueID, subject).Scan(&status, &lastSync)
			if err != nil {
				status = ""
				lastSync = nil
			}
			stale := status != "ready" || lastSync == nil || now.Sub(*lastSync) > time.Duration(minutes)*time.Minute
			if !stale {
				continue
			}
			if !r.subjectCooldownOK(ctx, alert, subject, now) {
				continue
			}
			payload := map[string]any{
				"wallet_address": subject,
				"venue_id":       m.venueID.String(),
				"event_type":     alert.Type,
				"minutes":        minutes,
			}
			dedupKey := fmt.Sprintf("%s|%s|%d", alert.ID, subject, now.Unix())
			r.createAlertEvent(ctx, alert, alert.Type, payload, dedupKey)
			if alert.NotifyTelegram {
				_ = r.enqueueTelegram(ctx, alert, fmt.Sprintf("Alert %s: wallet stale — %s", alert.Name, aliasFor(members, subject)), dedupKey)
			}
			triggeredAny = true
		}
	}

	// update last_evaluated_at (and last_triggered_at if triggered)
	var triggeredAt *time.Time
	if triggeredAny {
		t := now
		triggeredAt = &t
	}
	_, err = r.pool.Exec(ctx, `UPDATE group_alerts SET last_triggered_at = COALESCE($2, last_triggered_at),
		last_evaluated_at = $3, updated_at = NOW() WHERE id = $1`,
		alert.ID, triggeredAt, now)
	alert.LastEvaluatedAt = &now
	if triggeredAny {
		alert.LastTriggeredAt = triggeredAt
	}
	return err
}

func (r *Repository) groupMembers(ctx context.Context, groupID uuid.UUID) ([]tgMember, error) {
	rows, err := r.pool.Query(ctx, `SELECT m.venue_id, m.wallet_address, COALESCE(m.alias, f.alias, r.display_name, m.wallet_address)
		FROM trader_group_members m
		LEFT JOIN trader_follows f ON f.venue_id = m.venue_id AND f.wallet_address = m.wallet_address AND f.user_id = (SELECT user_id FROM trader_groups WHERE id = $1)
		LEFT JOIN trader_registry r ON r.venue_id = m.venue_id AND r.wallet_address = m.wallet_address
		WHERE m.group_id = $1`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []tgMember
	for rows.Next() {
		var m tgMember
		if err := rows.Scan(&m.venueID, &m.addr, &m.alias); err != nil {
			return nil, err
		}
		m.addr = strings.ToLower(strings.TrimSpace(m.addr))
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *Repository) subjectCooldownOK(ctx context.Context, alert *GroupAlert, subject string, now time.Time) bool {
	if alert.CooldownSeconds <= 0 {
		return true
	}
	prefix := fmt.Sprintf("%s|%s|", alert.ID, subject)
	var last *time.Time
	err := r.pool.QueryRow(ctx, `SELECT MAX(triggered_at) FROM group_alert_events WHERE alert_id = $1 AND dedup_key LIKE $2`,
		alert.ID, prefix+"%").Scan(&last)
	if err != nil || last == nil {
		return true
	}
	return now.Sub(*last) >= time.Duration(alert.CooldownSeconds)*time.Second
}

func aliasFor(members []tgMember, addr string) string {
	addr = strings.ToLower(strings.TrimSpace(addr))
	for _, m := range members {
		if strings.ToLower(strings.TrimSpace(m.addr)) == addr && m.alias != "" {
			return m.alias
		}
	}
	if addr == "" {
		return "unknown"
	}
	if len(addr) > 10 {
		return addr[:6] + "..." + addr[len(addr)-4:]
	}
	return addr
}
