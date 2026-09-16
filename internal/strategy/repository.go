package strategy

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Create(ctx context.Context, instance *StrategyInstance) error {
	query := `
		INSERT INTO strategy_instances (id, tenant_id, strategy_type_id, name, mode, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`

	_, err := r.pool.Exec(ctx, query,
		instance.ID,
		instance.TenantID,
		instance.StrategyTypeID,
		instance.Name,
		instance.Mode,
		instance.Status,
		instance.CreatedAt,
		instance.UpdatedAt,
	)
	return err
}

func (r *Repository) GetByID(ctx context.Context, id uuid.UUID) (*StrategyInstance, error) {
	query := `
		SELECT id, tenant_id, strategy_type_id, name, mode, status, created_at, updated_at
		FROM strategy_instances
		WHERE id = $1`

	instance := &StrategyInstance{}
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&instance.ID,
		&instance.TenantID,
		&instance.StrategyTypeID,
		&instance.Name,
		&instance.Mode,
		&instance.Status,
		&instance.CreatedAt,
		&instance.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return instance, nil
}

func (r *Repository) List(ctx context.Context, tenantID uuid.UUID) ([]*StrategyInstance, error) {
	query := `
		SELECT id, tenant_id, strategy_type_id, name, mode, status, created_at, updated_at
		FROM strategy_instances
		WHERE tenant_id = $1
		ORDER BY created_at DESC`

	rows, err := r.pool.Query(ctx, query, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var instances []*StrategyInstance
	for rows.Next() {
		instance := &StrategyInstance{}
		err := rows.Scan(
			&instance.ID,
			&instance.TenantID,
			&instance.StrategyTypeID,
			&instance.Name,
			&instance.Mode,
			&instance.Status,
			&instance.CreatedAt,
			&instance.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		instances = append(instances, instance)
	}
	return instances, nil
}

func (r *Repository) Update(ctx context.Context, instance *StrategyInstance) error {
	query := `
		UPDATE strategy_instances
		SET name = $2, status = $3, updated_at = $4
		WHERE id = $1`

	_, err := r.pool.Exec(ctx, query,
		instance.ID,
		instance.Name,
		instance.Status,
		time.Now(),
	)
	return err
}

func (r *Repository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM strategy_instances WHERE id = $1`
	_, err := r.pool.Exec(ctx, query, id)
	return err
}

func (r *Repository) CreateDecision(ctx context.Context, decision *StrategyDecision) error {
	var oppID interface{} = decision.OpportunityID
	if decision.OpportunityID == uuid.Nil {
		oppID = nil
	}

	query := `
		INSERT INTO strategy_decisions (id, strategy_instance_id, opportunity_id, decision, reason_code, reason_detail, decision_timestamp)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`

	_, err := r.pool.Exec(ctx, query,
		decision.ID,
		decision.StrategyID,
		oppID,
		decision.Action,
		"EXECUTED",
		"{}",
		decision.ExecutedAt,
	)
	return err
}

func (r *Repository) ListDecisions(ctx context.Context, strategyID uuid.UUID, limit int) ([]*StrategyDecision, error) {
	query := `
		SELECT id, strategy_instance_id, opportunity_id, decision, decision_timestamp, decision_timestamp
		FROM strategy_decisions
		WHERE strategy_instance_id = $1
		ORDER BY decision_timestamp DESC
		LIMIT $2`

	rows, err := r.pool.Query(ctx, query, strategyID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var decisions []*StrategyDecision
	for rows.Next() {
		decision := &StrategyDecision{}
		err := rows.Scan(
			&decision.ID,
			&decision.StrategyID,
			&decision.OpportunityID,
			&decision.Action,
			&decision.ExecutedAt,
			&decision.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		decisions = append(decisions, decision)
	}
	return decisions, nil
}

func (r *Repository) GetStrategyTypeByCode(ctx context.Context, code string) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.pool.QueryRow(ctx, "SELECT id FROM strategy_types WHERE code = $1", code).Scan(&id)
	return id, err
}

func (r *Repository) EnsureStrategyType(ctx context.Context, code, name string) (uuid.UUID, error) {
	id, err := r.GetStrategyTypeByCode(ctx, code)
	if err == nil {
		return id, nil
	}

	// Insert new strategy type
	err = r.pool.QueryRow(ctx,
		"INSERT INTO strategy_types (id, code, name, status) VALUES ($1, $2, $3, 'ACTIVE') RETURNING id",
		uuid.New(), code, name,
	).Scan(&id)
	return id, err
}
