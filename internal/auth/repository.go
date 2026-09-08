package auth

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

func (r *Repository) CreateUser(ctx context.Context, user *User) error {
	query := `
		INSERT INTO users (id, email, password_hash, role, status)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at, updated_at`

	return r.db.QueryRow(ctx, query,
		user.ID, user.Email, user.PasswordHash, user.Role, user.Status,
	).Scan(&user.ID, &user.CreatedAt, &user.UpdatedAt)
}

func (r *Repository) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	query := `
		SELECT id, tenant_id, email, password_hash, role, status, created_at, updated_at, last_login_at
		FROM users
		WHERE email = $1`

	user := &User{}
	err := r.db.QueryRow(ctx, query, email).Scan(
		&user.ID, &user.TenantID, &user.Email, &user.PasswordHash,
		&user.Role, &user.Status, &user.CreatedAt, &user.UpdatedAt, &user.LastLoginAt,
	)
	if err != nil {
		return nil, err
	}
	return user, nil
}

func (r *Repository) GetUserByID(ctx context.Context, id uuid.UUID) (*User, error) {
	query := `
		SELECT id, tenant_id, email, password_hash, role, status, created_at, updated_at, last_login_at
		FROM users
		WHERE id = $1`

	user := &User{}
	err := r.db.QueryRow(ctx, query, id).Scan(
		&user.ID, &user.TenantID, &user.Email, &user.PasswordHash,
		&user.Role, &user.Status, &user.CreatedAt, &user.UpdatedAt, &user.LastLoginAt,
	)
	if err != nil {
		return nil, err
	}
	return user, nil
}

func (r *Repository) UpdateLastLogin(ctx context.Context, userID uuid.UUID) error {
	query := `UPDATE users SET last_login_at = NOW(), updated_at = NOW() WHERE id = $1`
	_, err := r.db.Exec(ctx, query, userID)
	return err
}

func (r *Repository) UpdatePasswordHash(ctx context.Context, userID uuid.UUID, passwordHash string) error {
	query := `UPDATE users SET password_hash = $2, updated_at = NOW() WHERE id = $1`
	_, err := r.db.Exec(ctx, query, userID, passwordHash)
	return err
}

func (r *Repository) ExistsByEmail(ctx context.Context, email string) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM users WHERE email = $1)`
	var exists bool
	err := r.db.QueryRow(ctx, query, email).Scan(&exists)
	return exists, err
}

func (r *Repository) CreateRefreshToken(ctx context.Context, token *RefreshToken) error {
	query := `
		INSERT INTO refresh_tokens (id, user_id, token_hash, expires_at, revoked)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING created_at`

	return r.db.QueryRow(ctx, query,
		token.ID, token.UserID, token.TokenHash, token.ExpiresAt, token.Revoked,
	).Scan(&token.CreatedAt)
}

func (r *Repository) GetRefreshTokenByHash(ctx context.Context, tokenHash string) (*RefreshToken, error) {
	query := `
		SELECT id, user_id, token_hash, expires_at, revoked, created_at
		FROM refresh_tokens
		WHERE token_hash = $1`

	token := &RefreshToken{}
	err := r.db.QueryRow(ctx, query, tokenHash).Scan(
		&token.ID, &token.UserID, &token.TokenHash,
		&token.ExpiresAt, &token.Revoked, &token.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return token, nil
}

func (r *Repository) RevokeRefreshToken(ctx context.Context, tokenHash string) error {
	query := `UPDATE refresh_tokens SET revoked = TRUE WHERE token_hash = $1`
	_, err := r.db.Exec(ctx, query, tokenHash)
	return err
}

func (r *Repository) RevokeAllUserRefreshTokens(ctx context.Context, userID uuid.UUID) error {
	query := `UPDATE refresh_tokens SET revoked = TRUE WHERE user_id = $1 AND revoked = FALSE`
	_, err := r.db.Exec(ctx, query, userID)
	return err
}

func (r *Repository) DeleteExpiredRefreshTokens(ctx context.Context) (int64, error) {
	query := `DELETE FROM refresh_tokens WHERE expires_at < NOW() OR revoked = TRUE`
	result, err := r.db.Exec(ctx, query)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}
