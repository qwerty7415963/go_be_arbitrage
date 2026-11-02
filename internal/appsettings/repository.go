package appsettings

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RepositoryInterface is the persistence surface.
type RepositoryInterface interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key, value string, updatedBy *uuid.UUID) error
	GetAll(ctx context.Context) ([]Setting, error)
}

type Repository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Get(ctx context.Context, key string) (string, error) {
	var value string
	err := r.db.QueryRow(ctx, `SELECT value FROM app_settings WHERE key = $1`, key).Scan(&value)
	if err != nil {
		return "", err
	}
	return value, nil
}

func (r *Repository) Set(ctx context.Context, key, value string, updatedBy *uuid.UUID) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO app_settings (key, value, updated_by, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_by = EXCLUDED.updated_by, updated_at = NOW()`,
		key, value, updatedBy)
	return err
}

func (r *Repository) GetAll(ctx context.Context) ([]Setting, error) {
	rows, err := r.db.Query(ctx, `SELECT key, value, updated_by, updated_at FROM app_settings ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Setting{}
	for rows.Next() {
		var s Setting
		if err := rows.Scan(&s.Key, &s.Value, &s.UpdatedBy, &s.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SeedIfNotExists inserts a row only if missing.
func (r *Repository) SeedIfNotExists(ctx context.Context, key, value string) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO app_settings (key, value) VALUES ($1, $2)
		ON CONFLICT (key) DO NOTHING`, key, value)
	return err
}
