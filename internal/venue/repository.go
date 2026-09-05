package venue

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Create(ctx context.Context, v *Venue) error {
	capsJSON, err := json.Marshal(v.Capabilities)
	if err != nil {
		return fmt.Errorf("marshal capabilities: %w", err)
	}

	metaJSON, err := json.Marshal(v.Metadata)
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}

	query := `
		INSERT INTO venues (id, code, name, venue_type, status, capabilities, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING created_at, updated_at`

	return r.db.QueryRow(ctx, query,
		v.ID, v.Code, v.Name, v.VenueType, v.Status,
		capsJSON, metaJSON,
	).Scan(&v.CreatedAt, &v.UpdatedAt)
}

func (r *Repository) GetByID(ctx context.Context, id uuid.UUID) (*Venue, error) {
	query := `
		SELECT id, code, name, venue_type, status, capabilities, metadata, created_at, updated_at
		FROM venues
		WHERE id = $1`

	v := &Venue{}
	err := r.db.QueryRow(ctx, query, id).Scan(
		&v.ID, &v.Code, &v.Name, &v.VenueType, &v.Status,
		&v.Capabilities, &v.Metadata, &v.CreatedAt, &v.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return v, nil
}

func (r *Repository) GetByCode(ctx context.Context, code string) (*Venue, error) {
	query := `
		SELECT id, code, name, venue_type, status, capabilities, metadata, created_at, updated_at
		FROM venues
		WHERE code = $1`

	v := &Venue{}
	err := r.db.QueryRow(ctx, query, code).Scan(
		&v.ID, &v.Code, &v.Name, &v.VenueType, &v.Status,
		&v.Capabilities, &v.Metadata, &v.CreatedAt, &v.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return v, nil
}

func (r *Repository) List(ctx context.Context) ([]*Venue, error) {
	query := `
		SELECT id, code, name, venue_type, status, capabilities, metadata, created_at, updated_at
		FROM venues
		ORDER BY created_at DESC`

	rows, err := r.db.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var venues []*Venue
	for rows.Next() {
		v := &Venue{}
		err := rows.Scan(
			&v.ID, &v.Code, &v.Name, &v.VenueType, &v.Status,
			&v.Capabilities, &v.Metadata, &v.CreatedAt, &v.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		venues = append(venues, v)
	}
	return venues, rows.Err()
}

func (r *Repository) Update(ctx context.Context, v *Venue) error {
	capsJSON, err := json.Marshal(v.Capabilities)
	if err != nil {
		return fmt.Errorf("marshal capabilities: %w", err)
	}

	metaJSON, err := json.Marshal(v.Metadata)
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}

	query := `
		UPDATE venues
		SET name = $2, status = $3, capabilities = $4, metadata = $5, updated_at = NOW()
		WHERE id = $1
		RETURNING updated_at`

	return r.db.QueryRow(ctx, query,
		v.ID, v.Name, v.Status, capsJSON, metaJSON,
	).Scan(&v.UpdatedAt)
}

func (r *Repository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM venues WHERE id = $1`
	_, err := r.db.Exec(ctx, query, id)
	return err
}
