package auth

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Web3Repository struct {
	db *pgxpool.Pool
}

func NewWeb3Repository(db *pgxpool.Pool) *Web3Repository {
	return &Web3Repository{db: db}
}

// --- Wallet Addresses ---

func (r *Web3Repository) GetWalletByAddress(ctx context.Context, address string, chainID int64) (*WalletAddress, error) {
	query := `
		SELECT id, user_id, address, chain_id, is_primary, verified_at, created_at
		FROM wallet_addresses
		WHERE address = $1 AND chain_id = $2`

	w := &WalletAddress{}
	err := r.db.QueryRow(ctx, query, address, chainID).Scan(
		&w.ID, &w.UserID, &w.Address, &w.ChainID, &w.IsPrimary, &w.VerifiedAt, &w.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return w, nil
}

func (r *Web3Repository) GetWalletsByUserID(ctx context.Context, userID uuid.UUID) ([]*WalletAddress, error) {
	query := `
		SELECT id, user_id, address, chain_id, is_primary, verified_at, created_at
		FROM wallet_addresses
		WHERE user_id = $1
		ORDER BY is_primary DESC, created_at ASC`

	rows, err := r.db.Query(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var wallets []*WalletAddress
	for rows.Next() {
		w := &WalletAddress{}
		if err := rows.Scan(&w.ID, &w.UserID, &w.Address, &w.ChainID, &w.IsPrimary, &w.VerifiedAt, &w.CreatedAt); err != nil {
			return nil, err
		}
		wallets = append(wallets, w)
	}
	return wallets, rows.Err()
}

func (r *Web3Repository) CreateWallet(ctx context.Context, w *WalletAddress) error {
	query := `
		INSERT INTO wallet_addresses (id, user_id, address, chain_id, is_primary)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING verified_at, created_at`

	return r.db.QueryRow(ctx, query,
		w.ID, w.UserID, w.Address, w.ChainID, w.IsPrimary,
	).Scan(&w.VerifiedAt, &w.CreatedAt)
}

func (r *Web3Repository) DeleteWallet(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM wallet_addresses WHERE id = $1`
	_, err := r.db.Exec(ctx, query, id)
	return err
}

func (r *Web3Repository) SetPrimaryWallet(ctx context.Context, userID uuid.UUID, walletID uuid.UUID, chainID int64) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Unset all primary for this chain
	_, err = tx.Exec(ctx,
		`UPDATE wallet_addresses SET is_primary = FALSE WHERE user_id = $1 AND chain_id = $2`,
		userID, chainID,
	)
	if err != nil {
		return err
	}

	// Set new primary
	_, err = tx.Exec(ctx,
		`UPDATE wallet_addresses SET is_primary = TRUE WHERE id = $1 AND user_id = $2`,
		walletID, userID,
	)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (r *Web3Repository) IsWalletOwnedByUser(ctx context.Context, userID uuid.UUID, address string, chainID int64) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM wallet_addresses WHERE user_id = $1 AND address = $2 AND chain_id = $3)`
	var exists bool
	err := r.db.QueryRow(ctx, query, userID, address, chainID).Scan(&exists)
	return exists, err
}

func (r *Web3Repository) IsWalletOwnedBy(ctx context.Context, userID uuid.UUID, walletID uuid.UUID) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM wallet_addresses WHERE user_id = $1 AND id = $2)`
	var exists bool
	err := r.db.QueryRow(ctx, query, userID, walletID).Scan(&exists)
	return exists, err
}

// --- SIWE Nonces ---

func (r *Web3Repository) CreateNonce(ctx context.Context, n *WalletNonce) error {
	query := `
		INSERT INTO wallet_nonces (id, address, chain_id, nonce, expires_at)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING created_at`

	return r.db.QueryRow(ctx, query,
		n.ID, n.Address, n.ChainID, n.Nonce, n.ExpiresAt,
	).Scan(&n.CreatedAt)
}

func (r *Web3Repository) GetValidNonce(ctx context.Context, nonce string, address string, chainID int64) (*WalletNonce, error) {
	query := `
		SELECT id, address, chain_id, nonce, expires_at, used, created_at
		FROM wallet_nonces
		WHERE nonce = $1 AND address = $2 AND chain_id = $3 AND used = FALSE AND expires_at > NOW()`

	n := &WalletNonce{}
	err := r.db.QueryRow(ctx, query, nonce, address, chainID).Scan(
		&n.ID, &n.Address, &n.ChainID, &n.Nonce, &n.ExpiresAt, &n.Used, &n.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return n, nil
}

func (r *Web3Repository) MarkNonceUsed(ctx context.Context, id uuid.UUID) error {
	query := `UPDATE wallet_nonces SET used = TRUE WHERE id = $1`
	_, err := r.db.Exec(ctx, query, id)
	return err
}

func (r *Web3Repository) CleanupNonces(ctx context.Context) (int64, error) {
	query := `DELETE FROM wallet_nonces WHERE expires_at < NOW() OR (used = TRUE AND created_at < NOW() - INTERVAL '1 hour')`
	result, err := r.db.Exec(ctx, query)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

// --- User creation for wallet-first ---

func (r *Web3Repository) CreateUserWithWallet(ctx context.Context, user *User, wallet *WalletAddress) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Create user
	userQuery := `
		INSERT INTO users (id, auth_method, role, status)
		VALUES ($1, 'wallet', 'user', 'ACTIVE')
		RETURNING created_at, updated_at`
	if err := tx.QueryRow(ctx, userQuery, user.ID).Scan(&user.CreatedAt, &user.UpdatedAt); err != nil {
		return err
	}

	// Link wallet
	walletQuery := `
		INSERT INTO wallet_addresses (id, user_id, address, chain_id, is_primary)
		VALUES ($1, $2, $3, $4, TRUE)
		RETURNING verified_at, created_at`
	if err := tx.QueryRow(ctx, walletQuery,
		wallet.ID, user.ID, wallet.Address, wallet.ChainID,
	).Scan(&wallet.VerifiedAt, &wallet.CreatedAt); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (r *Web3Repository) GetUserByAddress(ctx context.Context, address string, chainID int64) (*User, error) {
	query := `
		SELECT u.id, u.tenant_id, u.email, u.password_hash, u.role, u.status, u.auth_method,
		       u.created_at, u.updated_at, u.last_login_at
		FROM users u
		INNER JOIN wallet_addresses w ON w.user_id = u.id
		WHERE w.address = $1 AND w.chain_id = $2`

	user := &User{}
	err := r.db.QueryRow(ctx, query, address, chainID).Scan(
		&user.ID, &user.TenantID, &user.Email, &user.PasswordHash,
		&user.Role, &user.Status, &user.AuthMethod,
		&user.CreatedAt, &user.UpdatedAt, &user.LastLoginAt,
	)
	if err != nil {
		return nil, err
	}
	return user, nil
}
