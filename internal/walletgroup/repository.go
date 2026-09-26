package walletgroup

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	// ErrDuplicateName is returned when (user_id, name) already exists.
	ErrDuplicateName = errors.New("duplicate group name")
	// ErrWalletNotFound is returned when a referenced wallet ID does not
	// exist in tracked_wallets.
	ErrWalletNotFound = errors.New("wallet not found")
)

type Repository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

const groupColumns = `g.id, g.user_id, g.name, g.description, g.color, g.created_at, g.updated_at`

func scanGroup(row pgx.Row) (*Group, error) {
	g := &Group{}
	err := row.Scan(&g.ID, &g.UserID, &g.Name, &g.Description, &g.Color, &g.CreatedAt, &g.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return g, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func (r *Repository) CreateGroup(ctx context.Context, g *Group) error {
	query := `
		INSERT INTO user_wallet_groups (id, user_id, name, description, color)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING created_at, updated_at`

	err := r.db.QueryRow(ctx, query,
		g.ID, g.UserID, g.Name, g.Description, g.Color,
	).Scan(&g.CreatedAt, &g.UpdatedAt)
	if isUniqueViolation(err) {
		return ErrDuplicateName
	}
	return err
}

// GetGroupByID returns the group with its wallet_count regardless of owner;
// the service layer enforces ownership.
func (r *Repository) GetGroupByID(ctx context.Context, id uuid.UUID) (*Group, error) {
	query := `
		SELECT ` + groupColumns + `, COUNT(m.wallet_id)
		FROM user_wallet_groups g
		LEFT JOIN group_wallet_members m ON m.group_id = g.id
		WHERE g.id = $1
		GROUP BY g.id`

	return scanGroupWithCount(r.db.QueryRow(ctx, query, id))
}

func (r *Repository) ListGroups(ctx context.Context, userID uuid.UUID) ([]*Group, error) {
	query := `
		SELECT ` + groupColumns + `, COUNT(m.wallet_id)
		FROM user_wallet_groups g
		LEFT JOIN group_wallet_members m ON m.group_id = g.id
		WHERE g.user_id = $1
		GROUP BY g.id
		ORDER BY g.created_at DESC, g.id DESC`

	rows, err := r.db.Query(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []*Group
	for rows.Next() {
		g, err := scanGroupWithCount(rows)
		if err != nil {
			return nil, err
		}
		groups = append(groups, g)
	}
	if groups == nil {
		groups = []*Group{}
	}
	return groups, rows.Err()
}

func scanGroupWithCount(row pgx.Row) (*Group, error) {
	g := &Group{}
	err := row.Scan(&g.ID, &g.UserID, &g.Name, &g.Description, &g.Color, &g.CreatedAt, &g.UpdatedAt, &g.WalletCount)
	if err != nil {
		return nil, err
	}
	return g, nil
}

func (r *Repository) UpdateGroup(ctx context.Context, g *Group) error {
	query := `
		UPDATE user_wallet_groups
		SET name = $2, description = $3, color = $4, updated_at = NOW()
		WHERE id = $1
		RETURNING updated_at`

	err := r.db.QueryRow(ctx, query, g.ID, g.Name, g.Description, g.Color).Scan(&g.UpdatedAt)
	if isUniqueViolation(err) {
		return ErrDuplicateName
	}
	return err
}

// DeleteGroup removes the group for its owner; memberships cascade (BR-10:
// tracked_wallets are never touched). Returns rows affected.
func (r *Repository) DeleteGroup(ctx context.Context, id, userID uuid.UUID) (int64, error) {
	tag, err := r.db.Exec(ctx,
		`DELETE FROM user_wallet_groups WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// AddMembersTx resolves every item and inserts memberships in one
// transaction: any unknown wallet or failure rolls the whole batch back
// (no partial membership). Address items upsert their tracked_wallets
// identity first (BR-01). Returns the number of memberships actually
// created (idempotent re-adds count 0).
func (r *Repository) AddMembersTx(ctx context.Context, groupID, addedBy uuid.UUID, items []walletItem) (int64, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit

	var created int64
	for _, item := range items {
		walletID, err := resolveWalletTx(ctx, tx, item)
		if err != nil {
			return 0, err
		}

		tag, err := tx.Exec(ctx, `
			INSERT INTO group_wallet_members (group_id, wallet_id, added_by)
			VALUES ($1, $2, $3)
			ON CONFLICT (group_id, wallet_id) DO NOTHING`,
			groupID, walletID, addedBy)
		if err != nil {
			return 0, err
		}
		created += tag.RowsAffected()
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return created, nil
}

func resolveWalletTx(ctx context.Context, tx pgx.Tx, item walletItem) (uuid.UUID, error) {
	if item.isID {
		var id uuid.UUID
		err := tx.QueryRow(ctx, `SELECT id FROM tracked_wallets WHERE id = $1`, item.id).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, ErrWalletNotFound
		}
		return id, err
	}

	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		INSERT INTO tracked_wallets (chain, address)
		VALUES ($1, $2)
		ON CONFLICT (chain, address) DO UPDATE SET last_seen_at = NOW()
		RETURNING id`,
		item.chain, item.address).Scan(&id)
	return id, err
}

// RemoveMembers deletes memberships for ID or address items. Absent
// memberships are a no-op (idempotent, BR-09). Returns rows removed.
func (r *Repository) RemoveMembers(ctx context.Context, groupID uuid.UUID, items []walletItem) (int64, error) {
	var ids []uuid.UUID
	var chains, addresses []string
	for _, item := range items {
		if item.isID {
			ids = append(ids, item.id)
			continue
		}
		chains = append(chains, item.chain)
		addresses = append(addresses, item.address)
	}
	if len(ids) == 0 && len(addresses) == 0 {
		return 0, nil
	}

	query := `
		DELETE FROM group_wallet_members
		WHERE group_id = $1
		  AND (wallet_id = ANY($2)
		       OR wallet_id IN (
		           SELECT id FROM tracked_wallets
		           WHERE (chain, address) IN (
		               SELECT * FROM unnest($3::text[], $4::text[])
		           )
		       ))`

	tag, err := r.db.Exec(ctx, query, groupID, ids, chains, addresses)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// ListMembers returns one page of a group's wallets plus the total count.
// search is a case-insensitive partial address match (already normalized).
func (r *Repository) ListMembers(ctx context.Context, groupID uuid.UUID, search string, limit, offset int) ([]*WalletRef, int64, error) {
	countQuery := `
		SELECT COUNT(*)
		FROM group_wallet_members m
		JOIN tracked_wallets w ON w.id = m.wallet_id
		WHERE m.group_id = $1
		  AND ($2 = '' OR w.address ILIKE '%' || $2 || '%')`

	var total int64
	if err := r.db.QueryRow(ctx, countQuery, groupID, search).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := `
		SELECT w.id, w.chain, w.address, m.added_at
		FROM group_wallet_members m
		JOIN tracked_wallets w ON w.id = m.wallet_id
		WHERE m.group_id = $1
		  AND ($2 = '' OR w.address ILIKE '%' || $2 || '%')
		ORDER BY w.address ASC, w.chain ASC
		LIMIT $3 OFFSET $4`

	rows, err := r.db.Query(ctx, query, groupID, search, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var wallets []*WalletRef
	for rows.Next() {
		w := &WalletRef{}
		if err := rows.Scan(&w.ID, &w.Chain, &w.Address, &w.AddedAt); err != nil {
			return nil, 0, err
		}
		wallets = append(wallets, w)
	}
	if wallets == nil {
		wallets = []*WalletRef{}
	}
	return wallets, total, rows.Err()
}
