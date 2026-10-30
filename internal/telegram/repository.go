package telegram

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

// parseCtx extracts a context (defaults to Background) and returns remaining args.
func parseCtx(args []interface{}) (context.Context, []interface{}) {
	if len(args) > 0 {
		if c, ok := args[0].(context.Context); ok {
			return c, args[1:]
		}
	}
	return context.Background(), args
}

func parseUUID(v interface{}) (uuid.UUID, error) {
	switch val := v.(type) {
	case uuid.UUID:
		return val, nil
	case string:
		u, err := uuid.Parse(val)
		if err != nil {
			return uuid.Nil, err
		}
		return u, nil
	default:
		return uuid.Nil, fmt.Errorf("expected uuid, got %T", v)
	}
}

func parseChatID(v interface{}) (int64, error) {
	switch val := v.(type) {
	case int64:
		return val, nil
	case int:
		return int64(val), nil
	case float64:
		return int64(val), nil
	case string:
		// best effort
		var n int64
		_, err := fmt.Sscan(val, &n)
		return n, err
	default:
		return 0, fmt.Errorf("expected chat id, got %T", v)
	}
}

func (r *Repository) UpsertBinding(args ...interface{}) error {
	ctx, args := parseCtx(args)
	// support (*Binding) or positional
	if len(args) == 1 {
		if b, ok := args[0].(*Binding); ok {
			if b == nil {
				return errors.New("binding is nil")
			}
			if err := b.Validate(); err != nil {
				return err
			}
			locale := string(b.Locale)
			if locale == "" {
				locale = string(LocaleVI)
			}
			status := string(b.Status)
			if status == "" {
				status = string(BindingStatusActive)
			}
			var username, firstName interface{}
			if b.TGUsername != nil {
				username = *b.TGUsername
			}
			if b.TGFirstName != nil {
				firstName = *b.TGFirstName
			}
			_, err := r.pool.Exec(ctx, `
				INSERT INTO telegram_bindings (user_id, chat_id, tg_username, tg_first_name, locale, status, updated_at)
				VALUES ($1, $2, $3, $4, $5, $6, NOW())
				ON CONFLICT (user_id) DO UPDATE SET
					chat_id = EXCLUDED.chat_id,
					tg_username = EXCLUDED.tg_username,
					tg_first_name = EXCLUDED.tg_first_name,
					locale = EXCLUDED.locale,
					status = EXCLUDED.status,
					updated_at = NOW()`,
				b.UserID, b.ChatID, username, firstName, locale, status)
			return err
		}
	}
	// positional: userID, chatID, [tgUsername string/*string], [tgFirstName], [locale], [status]
	c, rest := parseCtx(args)
	_ = c
	if len(rest) < 2 {
		return errors.New("UpsertBinding: missing arguments")
	}
	userID, err := parseUUID(rest[0])
	if err != nil {
		return err
	}
	chatID, err := parseChatID(rest[1])
	if err != nil {
		return err
	}
	var username *string
	var firstName *string
	locale := LocaleVI
	status := BindingStatusActive
	_ = status
	idx := 2
	if idx < len(rest) {
		switch v := rest[idx].(type) {
		case string:
			s := v
			username = &s
		case *string:
			username = v
		}
		idx++
	}
	if idx < len(rest) {
		switch v := rest[idx].(type) {
		case string:
			s := v
			firstName = &s
		case *string:
			firstName = v
		}
		idx++
	}
	if idx < len(rest) {
		if l, ok := rest[idx].(Locale); ok {
			locale = l
		} else if s, ok := rest[idx].(string); ok {
			locale = Locale(s)
		}
		idx++
	}
	if idx < len(rest) {
		if s, ok := rest[idx].(BindingStatus); ok {
			status = s
		} else if s, ok := rest[idx].(string); ok {
			status = BindingStatus(s)
		}
	}
	b := &Binding{UserID: userID, ChatID: chatID, TGUsername: username, TGFirstName: firstName, Locale: locale, Status: status}
	return r.UpsertBinding(ctx, b)
}

// UpsertBindingCtx is helper kept for compatibility.
func (r *Repository) UpsertBindingCtx(ctx context.Context, b *Binding) error {
	return r.UpsertBinding(ctx, b)
}

func (r *Repository) GetBinding(args ...interface{}) (*Binding, error) {
	ctx, rest := parseCtx(args)
	if len(rest) < 1 {
		return nil, errors.New("GetBinding: missing user id")
	}
	userID, err := parseUUID(rest[0])
	if err != nil {
		return nil, err
	}
	row := r.pool.QueryRow(ctx, `
		SELECT user_id, chat_id, tg_username, tg_first_name, locale, status, created_at, updated_at
		FROM telegram_bindings WHERE user_id = $1`, userID)
	var b Binding
	var locale, status string
	var username, firstName sql.NullString
	if err := row.Scan(&b.UserID, &b.ChatID, &username, &firstName, &locale, &status, &b.CreatedAt, &b.UpdatedAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, pgx.ErrNoRows
		}
		return nil, err
	}
	if username.Valid {
		s := username.String
		b.TGUsername = &s
	}
	if firstName.Valid {
		s := firstName.String
		b.TGFirstName = &s
	}
	b.Locale = Locale(locale)
	b.Status = BindingStatus(status)
	return &b, nil
}

func (r *Repository) GetBindingByChatID(args ...interface{}) (*Binding, error) {
	ctx, rest := parseCtx(args)
	if len(rest) < 1 {
		return nil, errors.New("GetBindingByChatID: missing chat id")
	}
	chatID, err := parseChatID(rest[0])
	if err != nil {
		return nil, err
	}
	row := r.pool.QueryRow(ctx, `
		SELECT user_id, chat_id, tg_username, tg_first_name, locale, status, created_at, updated_at
		FROM telegram_bindings WHERE chat_id = $1`, chatID)
	var b Binding
	var locale, status string
	var username, firstName sql.NullString
	if err := row.Scan(&b.UserID, &b.ChatID, &username, &firstName, &locale, &status, &b.CreatedAt, &b.UpdatedAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, pgx.ErrNoRows
		}
		return nil, err
	}
	if username.Valid {
		s := username.String
		b.TGUsername = &s
	}
	if firstName.Valid {
		s := firstName.String
		b.TGFirstName = &s
	}
	b.Locale = Locale(locale)
	b.Status = BindingStatus(status)
	return &b, nil
}

func (r *Repository) DeleteBinding(args ...interface{}) error {
	ctx, rest := parseCtx(args)
	if len(rest) < 1 {
		return errors.New("DeleteBinding: missing user id")
	}
	userID, err := parseUUID(rest[0])
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `DELETE FROM telegram_bindings WHERE user_id = $1`, userID)
	return err
}

func (r *Repository) CreateCode(args ...interface{}) error {
	ctx, args := parseCtx(args)
	// support (*LinkCode) or positional (code, userID, locale, expiresAt)
	if len(args) == 1 {
		if lc, ok := args[0].(*LinkCode); ok {
			if lc == nil {
				return errors.New("link code is nil")
			}
			if err := lc.Validate(); err != nil {
				return err
			}
			_, err := r.pool.Exec(ctx, `
				INSERT INTO telegram_link_codes (code, user_id, locale, expires_at, used_at, created_at)
				VALUES ($1, $2, $3, $4, $5, COALESCE($6, NOW()))`,
				lc.Code, lc.UserID, string(lc.Locale), lc.ExpiresAt, lc.UsedAt, lc.CreatedAt)
			return err
		}
	}
	// positional without ctx? already ctx fixed; args may be (code, userID, ...)
	// If called as CreateCode(ctx, code, userID, locale, expiresAt)
	if len(args) >= 4 {
		code, _ := args[0].(string)
		userID, err := parseUUID(args[1])
		if err != nil {
			return err
		}
		locale := LocaleVI
		if l, ok := args[2].(Locale); ok {
			locale = l
		} else if s, ok := args[2].(string); ok {
			locale = Locale(s)
		}
		var expiresAt time.Time
		switch v := args[3].(type) {
		case time.Time:
			expiresAt = v
		}
		lc := &LinkCode{Code: code, UserID: userID, Locale: locale, ExpiresAt: expiresAt}
		return r.CreateCode(ctx, lc)
	}
	// if ctx not passed? handle parseCtx style
	c, rest := parseCtx(args)
	if len(rest) >= 1 {
		if lc, ok := rest[0].(*LinkCode); ok {
			return r.CreateCode(c, lc)
		}
	}
	return errors.New("CreateCode: invalid arguments")
}

func (r *Repository) ConsumeCode(args ...interface{}) (*LinkCode, error) {
	ctx, rest := parseCtx(args)
	if len(rest) < 1 {
		return nil, errors.New("ConsumeCode: missing code")
	}
	code, ok := rest[0].(string)
	if !ok {
		return nil, errors.New("ConsumeCode: code must be string")
	}
	var lc LinkCode
	var locale string
	var usedAt sql.NullTime
	row := r.pool.QueryRow(ctx, `
		UPDATE telegram_link_codes
		SET used_at = NOW()
		WHERE code = $1 AND used_at IS NULL AND expires_at > NOW()
		RETURNING code, user_id, locale, expires_at, used_at, created_at`, code)
	var username interface{}
	_ = username
	if err := row.Scan(&lc.Code, &lc.UserID, &locale, &lc.ExpiresAt, &usedAt, &lc.CreatedAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, domain.NewError(domain.ErrCodeValidation, "invalid or expired code")
		}
		return nil, err
	}
	lc.Locale = Locale(locale)
	if usedAt.Valid {
		t := usedAt.Time
		lc.UsedAt = &t
	}
	return &lc, nil
}

func (r *Repository) MarkBlocked(args ...interface{}) error {
	ctx, rest := parseCtx(args)
	if len(rest) < 1 {
		return errors.New("MarkBlocked: missing chat id")
	}
	chatID, err := parseChatID(rest[0])
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `UPDATE telegram_bindings SET status = 'blocked', updated_at = NOW() WHERE chat_id = $1`, chatID)
	return err
}

// helper methods

func (r *Repository) DeleteUnusedCodes(ctx context.Context, userID uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM telegram_link_codes WHERE user_id = $1 AND used_at IS NULL`, userID)
	return err
}

func (r *Repository) GetActiveCode(ctx context.Context, userID uuid.UUID) (*LinkCode, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT code, user_id, locale, expires_at, used_at, created_at
		FROM telegram_link_codes
		WHERE user_id = $1 AND used_at IS NULL AND expires_at > NOW()
		ORDER BY created_at DESC LIMIT 1`, userID)
	var lc LinkCode
	var locale string
	var usedAt sql.NullTime
	if err := row.Scan(&lc.Code, &lc.UserID, &locale, &lc.ExpiresAt, &usedAt, &lc.CreatedAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, pgx.ErrNoRows
		}
		return nil, err
	}
	lc.Locale = Locale(locale)
	if usedAt.Valid {
		t := usedAt.Time
		lc.UsedAt = &t
	}
	return &lc, nil
}

func (r *Repository) CountEnabledGroups(ctx context.Context, userID uuid.UUID) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM trader_groups WHERE user_id = $1 AND notify_enabled = TRUE`, userID).Scan(&n)
	return n, err
}

func (r *Repository) CountWatchedWallets(ctx context.Context, userID uuid.UUID) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(DISTINCT (m.venue_id, m.wallet_address))
		FROM trader_group_members m
		JOIN trader_groups g ON g.id = m.group_id
		WHERE g.user_id = $1`, userID).Scan(&n)
	return n, err
}

func (r *Repository) GetUserEmail(ctx context.Context, userID uuid.UUID) (string, error) {
	var email string
	err := r.pool.QueryRow(ctx, `SELECT email FROM users WHERE id = $1`, userID).Scan(&email)
	return email, err
}

// CreateCode variadic wrapper already handles ctx. Add alias methods for direct ctx usage.
func (r *Repository) CreateCodeCtx(ctx context.Context, lc *LinkCode) error {
	return r.CreateCode(ctx, lc)
}
func (r *Repository) ConsumeCodeCtx(ctx context.Context, code string) (*LinkCode, error) {
	return r.ConsumeCode(ctx, code)
}
