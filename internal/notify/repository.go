package notify

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/qwerty7415963/go_be_arbitrage/internal/telegram"
)

// OutboxRow is one pending/sent telegram message.
type OutboxRow struct {
	ID            int64
	UserID        uuid.UUID
	ChatID        int64
	DedupKey      *string
	Body          string
	Attempts      int
	NextAttemptAt time.Time
	SentAt        *time.Time
	LastError     *string
	CreatedAt     time.Time
}

type NotifyTarget struct {
	UserID    uuid.UUID
	GroupName string
	Who       string
	ChatID    int64
	Locale    telegram.Locale
	Wallet    string
}

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(args ...interface{}) *Repository {
	r := &Repository{}
	for _, a := range args {
		switch v := a.(type) {
		case *pgxpool.Pool:
			r.pool = v
		}
	}
	return r
}

func (r *Repository) WatchedWallets(ctx context.Context) ([]string, error) {
	if r == nil || r.pool == nil {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT DISTINCT m.wallet_address
		FROM trader_group_members m
		JOIN trader_groups g ON g.id = m.group_id
		JOIN telegram_bindings b ON b.user_id = g.user_id
		WHERE g.notify_enabled = TRUE AND b.status = 'active'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var w string
		if err := rows.Scan(&w); err != nil {
			return nil, err
		}
		out = append(out, strings.ToLower(strings.TrimSpace(w)))
	}
	return out, rows.Err()
}

func (r *Repository) TargetsForWallet(ctx context.Context, wallet string) ([]NotifyTarget, error) {
	if r == nil || r.pool == nil {
		return nil, nil
	}
	wallet = strings.ToLower(strings.TrimSpace(wallet))
	rows, err := r.pool.Query(ctx, `
		SELECT g.user_id, g.name, COALESCE(m.alias, m.display_name, m.wallet_address) AS who,
		       b.chat_id, b.locale, m.wallet_address
		FROM trader_group_members m
		JOIN trader_groups g ON g.id = m.group_id
		JOIN telegram_bindings b ON b.user_id = g.user_id
		WHERE m.wallet_address = $1 AND g.notify_enabled = TRUE AND b.status = 'active'`, wallet)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []NotifyTarget{}
	for rows.Next() {
		var t NotifyTarget
		var locale string
		var who sql.NullString
		var groupName string
		if err := rows.Scan(&t.UserID, &groupName, &who, &t.ChatID, &locale, &t.Wallet); err != nil {
			return nil, err
		}
		t.GroupName = groupName
		if who.Valid {
			t.Who = who.String
		} else {
			t.Who = wallet
		}
		t.Locale = telegram.Locale(locale)
		if !t.Locale.IsValid() {
			t.Locale = telegram.LocaleVI
		}
		t.Wallet = strings.ToLower(strings.TrimSpace(t.Wallet))
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *Repository) HasDedupKey(ctx context.Context, key string) (bool, error) {
	if r == nil || r.pool == nil {
		return false, nil
	}
	if strings.TrimSpace(key) == "" {
		return false, nil
	}
	var exists bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM telegram_outbox WHERE dedup_key = $1)`, key).Scan(&exists)
	return exists, err
}

func (r *Repository) InsertOutbox(ctx context.Context, row *OutboxRow) (bool, error) {
	if r == nil || r.pool == nil {
		return false, fmt.Errorf("repo not configured")
	}
	if row == nil {
		return false, fmt.Errorf("row is nil")
	}
	next := row.NextAttemptAt
	if next.IsZero() {
		next = time.Now()
	}
	tag, err := r.pool.Exec(ctx, `
		INSERT INTO telegram_outbox (user_id, chat_id, dedup_key, body, attempts, next_attempt_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (dedup_key) WHERE dedup_key IS NOT NULL DO NOTHING`,
		row.UserID, row.ChatID, row.DedupKey, row.Body, row.Attempts, next)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *Repository) ListPendingOutbox(ctx context.Context) ([]OutboxRow, error) {
	if r == nil || r.pool == nil {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, user_id, chat_id, dedup_key, body, attempts, next_attempt_at, sent_at, last_error, created_at
		FROM telegram_outbox
		WHERE sent_at IS NULL AND attempts < 5 AND next_attempt_at <= NOW()
		ORDER BY next_attempt_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []OutboxRow{}
	for rows.Next() {
		var row OutboxRow
		var dedup sql.NullString
		var sentAt sql.NullTime
		var lastErr sql.NullString
		if err := rows.Scan(&row.ID, &row.UserID, &row.ChatID, &dedup, &row.Body, &row.Attempts, &row.NextAttemptAt, &sentAt, &lastErr, &row.CreatedAt); err != nil {
			return nil, err
		}
		if dedup.Valid {
			s := dedup.String
			row.DedupKey = &s
		}
		if sentAt.Valid {
			t := sentAt.Time
			row.SentAt = &t
		}
		if lastErr.Valid {
			s := lastErr.String
			row.LastError = &s
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *Repository) MarkSent(ctx context.Context, id int64) error {
	if r == nil || r.pool == nil {
		return nil
	}
	_, err := r.pool.Exec(ctx, `UPDATE telegram_outbox SET sent_at = NOW(), last_error = NULL WHERE id = $1`, id)
	return err
}

func (r *Repository) MarkAttempt(ctx context.Context, id int64, attempts int, next time.Time, lastErr string) error {
	if r == nil || r.pool == nil {
		return nil
	}
	_, err := r.pool.Exec(ctx, `UPDATE telegram_outbox SET attempts = $2, next_attempt_at = $3, last_error = $4 WHERE id = $1`, id, attempts, next, lastErr)
	return err
}

// aliases
func (r *Repository) GetPendingOutbox(ctx context.Context) ([]OutboxRow, error) {
	return r.ListPendingOutbox(ctx)
}
func (r *Repository) FetchPending(ctx context.Context) ([]OutboxRow, error) {
	return r.ListPendingOutbox(ctx)
}
func (r *Repository) MarkFailed(ctx context.Context, id int64, attempts int, next time.Time, lastErr string) error {
	return r.MarkAttempt(ctx, id, attempts, next, lastErr)
}
func (r *Repository) EnqueueOutbox(ctx context.Context, row *OutboxRow) (bool, error) {
	return r.InsertOutbox(ctx, row)
}
func (r *Repository) Insert(ctx context.Context, row *OutboxRow) (bool, error) {
	return r.InsertOutbox(ctx, row)
}
func (r *Repository) Exists(ctx context.Context, key string) (bool, error) {
	return r.HasDedupKey(ctx, key)
}
func (r *Repository) Watched(ctx context.Context) ([]string, error) { return r.WatchedWallets(ctx) }
func (r *Repository) Targets(ctx context.Context, wallet string) ([]NotifyTarget, error) {
	return r.TargetsForWallet(ctx, wallet)
}
func (r *Repository) HasDedup(ctx context.Context, key string) (bool, error) {
	return r.HasDedupKey(ctx, key)
}
func (r *Repository) DedupExists(ctx context.Context, key string) (bool, error) {
	return r.HasDedupKey(ctx, key)
}
func (r *Repository) InsertOutboxRow(ctx context.Context, row *OutboxRow) (bool, error) {
	return r.InsertOutbox(ctx, row)
}
func (r *Repository) ListPending(ctx context.Context) ([]OutboxRow, error) {
	return r.ListPendingOutbox(ctx)
}
func (r *Repository) Pending(ctx context.Context) ([]OutboxRow, error) {
	return r.ListPendingOutbox(ctx)
}
