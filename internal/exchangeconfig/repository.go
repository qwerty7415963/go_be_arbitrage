package exchangeconfig

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

func (r *Repository) Create(ctx context.Context, config *ExchangeConfig) error {
	query := `
		INSERT INTO exchange_configs (venue_id, exchange_name, rest_base_url, ws_url, status, rate_limit_rpm, timeout_ms, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, created_at, updated_at`

	return r.db.QueryRow(ctx, query,
		config.VenueID, config.ExchangeName, config.RestBaseURL, config.WsURL,
		config.Status, config.RateLimitRPM, config.TimeoutMs, config.CreatedBy,
	).Scan(&config.ID, &config.CreatedAt, &config.UpdatedAt)
}

func (r *Repository) GetByID(ctx context.Context, id uuid.UUID) (*ExchangeConfig, error) {
	query := `
		SELECT id, venue_id, exchange_name, rest_base_url, ws_url, status, rate_limit_rpm, timeout_ms, created_by, created_at, updated_at
		FROM exchange_configs
		WHERE id = $1`

	config := &ExchangeConfig{}
	err := r.db.QueryRow(ctx, query, id).Scan(
		&config.ID, &config.VenueID, &config.ExchangeName, &config.RestBaseURL, &config.WsURL,
		&config.Status, &config.RateLimitRPM, &config.TimeoutMs, &config.CreatedBy,
		&config.CreatedAt, &config.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return config, nil
}

func (r *Repository) GetByVenueID(ctx context.Context, venueID uuid.UUID) (*ExchangeConfig, error) {
	query := `
		SELECT id, venue_id, exchange_name, rest_base_url, ws_url, status, rate_limit_rpm, timeout_ms, created_by, created_at, updated_at
		FROM exchange_configs
		WHERE venue_id = $1`

	config := &ExchangeConfig{}
	err := r.db.QueryRow(ctx, query, venueID).Scan(
		&config.ID, &config.VenueID, &config.ExchangeName, &config.RestBaseURL, &config.WsURL,
		&config.Status, &config.RateLimitRPM, &config.TimeoutMs, &config.CreatedBy,
		&config.CreatedAt, &config.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return config, nil
}

func (r *Repository) List(ctx context.Context, limit, offset int) ([]*ExchangeConfig, error) {
	query := `
		SELECT id, venue_id, exchange_name, rest_base_url, ws_url, status, rate_limit_rpm, timeout_ms, created_by, created_at, updated_at
		FROM exchange_configs
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2`

	rows, err := r.db.Query(ctx, query, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var configs []*ExchangeConfig
	for rows.Next() {
		config := &ExchangeConfig{}
		err := rows.Scan(
			&config.ID, &config.VenueID, &config.ExchangeName, &config.RestBaseURL, &config.WsURL,
			&config.Status, &config.RateLimitRPM, &config.TimeoutMs, &config.CreatedBy,
			&config.CreatedAt, &config.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		configs = append(configs, config)
	}
	return configs, rows.Err()
}

func (r *Repository) Update(ctx context.Context, id uuid.UUID, config *ExchangeConfig) error {
	query := `
		UPDATE exchange_configs
		SET exchange_name = $2, rest_base_url = $3, ws_url = $4, status = $5,
			rate_limit_rpm = $6, timeout_ms = $7, updated_at = NOW()
		WHERE id = $1
		RETURNING updated_at`

	return r.db.QueryRow(ctx, query, id,
		config.ExchangeName, config.RestBaseURL, config.WsURL,
		config.Status, config.RateLimitRPM, config.TimeoutMs,
	).Scan(&config.UpdatedAt)
}

func (r *Repository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM exchange_configs WHERE id = $1`
	_, err := r.db.Exec(ctx, query, id)
	return err
}

func (r *Repository) ExistsByVenueID(ctx context.Context, venueID uuid.UUID) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM exchange_configs WHERE venue_id = $1)`
	var exists bool
	err := r.db.QueryRow(ctx, query, venueID).Scan(&exists)
	return exists, err
}
