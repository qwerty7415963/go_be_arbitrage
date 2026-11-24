package tradergroup

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// MarkAlertEventRead marks a single alert event as read. Ownership is verified
// via alert -> group -> user. Idempotent: an already-read event is left
// untouched.
func (r *Repository) MarkAlertEventRead(ctx context.Context, eventID, userID uuid.UUID) error {
	var owner uuid.UUID
	err := r.pool.QueryRow(ctx, `
		SELECT g.user_id
		FROM group_alert_events e
		JOIN group_alerts a ON a.id = e.alert_id
		JOIN trader_groups g ON g.id = a.group_id
		WHERE e.id = $1`, eventID).Scan(&owner)
	if err != nil {
		if err == pgx.ErrNoRows {
			return notFound()
		}
		return err
	}
	if owner != userID {
		return forbidden()
	}

	_, err = r.pool.Exec(ctx, `
		UPDATE group_alert_events
		SET read_at = NOW()
		WHERE id = $1 AND read_at IS NULL`, eventID)
	return err
}

// MarkAllAlertEventsRead marks every unread event in an owned group as read
// and returns the number of events updated.
func (r *Repository) MarkAllAlertEventsRead(ctx context.Context, groupID, userID uuid.UUID) (int, error) {
	if err := r.requireOwner(ctx, groupID, userID); err != nil {
		return 0, err
	}

	tag, err := r.pool.Exec(ctx, `
		UPDATE group_alert_events
		SET read_at = NOW()
		WHERE group_id = $1 AND read_at IS NULL`, groupID)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

// UnreadAlertCount returns the total number of unread alert events for the user
// plus a per-group breakdown (only groups with unread events).
func (r *Repository) UnreadAlertCount(ctx context.Context, userID uuid.UUID) (int, map[uuid.UUID]int, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT e.group_id, COUNT(*)::int
		FROM group_alert_events e
		JOIN trader_groups g ON g.id = e.group_id
		WHERE g.user_id = $1 AND e.read_at IS NULL
		GROUP BY e.group_id`, userID)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()

	counts := map[uuid.UUID]int{}
	total := 0
	for rows.Next() {
		var groupID uuid.UUID
		var n int
		if err := rows.Scan(&groupID, &n); err != nil {
			return 0, nil, err
		}
		counts[groupID] = n
		total += n
	}
	if err := rows.Err(); err != nil {
		return 0, nil, err
	}
	return total, counts, nil
}
