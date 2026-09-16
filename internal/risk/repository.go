package risk

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

func (r *Repository) CreatePolicy(ctx context.Context, policy *RiskPolicy) error {
	query := `
		INSERT INTO risk_policies (id, tenant_id, name, policy_type, config, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`

	_, err := r.pool.Exec(ctx, query,
		policy.ID,
		policy.TenantID,
		policy.Name,
		policy.PolicyType,
		policy.Config,
		policy.Status,
		policy.CreatedAt,
		policy.UpdatedAt,
	)
	return err
}

func (r *Repository) GetPolicyByID(ctx context.Context, id uuid.UUID) (*RiskPolicy, error) {
	query := `
		SELECT id, tenant_id, name, policy_type, config, status, created_at, updated_at
		FROM risk_policies
		WHERE id = $1`

	policy := &RiskPolicy{}
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&policy.ID,
		&policy.TenantID,
		&policy.Name,
		&policy.PolicyType,
		&policy.Config,
		&policy.Status,
		&policy.CreatedAt,
		&policy.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return policy, nil
}

func (r *Repository) ListPolicies(ctx context.Context, tenantID uuid.UUID) ([]*RiskPolicy, error) {
	query := `
		SELECT id, tenant_id, name, policy_type, config, status, created_at, updated_at
		FROM risk_policies
		WHERE tenant_id = $1 AND status = 'ACTIVE'
		ORDER BY created_at DESC`

	rows, err := r.pool.Query(ctx, query, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var policies []*RiskPolicy
	for rows.Next() {
		policy := &RiskPolicy{}
		err := rows.Scan(
			&policy.ID,
			&policy.TenantID,
			&policy.Name,
			&policy.PolicyType,
			&policy.Config,
			&policy.Status,
			&policy.CreatedAt,
			&policy.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		policies = append(policies, policy)
	}
	return policies, nil
}

func (r *Repository) UpdatePolicy(ctx context.Context, policy *RiskPolicy) error {
	query := `
		UPDATE risk_policies
		SET name = $2, config = $3, status = $4, updated_at = $5
		WHERE id = $1`

	_, err := r.pool.Exec(ctx, query,
		policy.ID,
		policy.Name,
		policy.Config,
		policy.Status,
		time.Now(),
	)
	return err
}

func (r *Repository) DeletePolicy(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM risk_policies WHERE id = $1`
	_, err := r.pool.Exec(ctx, query, id)
	return err
}

func (r *Repository) CreateCheck(ctx context.Context, check *RiskCheck) error {
	query := `
		INSERT INTO risk_checks (id, risk_decision_id, check_type, result, observed_value, threshold_value, reason_code, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`

	_, err := r.pool.Exec(ctx, query,
		check.ID,
		check.RiskDecisionID,
		check.CheckType,
		check.Result,
		check.ObservedValue,
		check.ThresholdValue,
		check.ReasonCode,
		check.CreatedAt,
	)
	return err
}

func (r *Repository) ListChecksByDecision(ctx context.Context, decisionID uuid.UUID) ([]*RiskCheck, error) {
	query := `
		SELECT id, risk_decision_id, check_type, result, observed_value, threshold_value, reason_code, created_at
		FROM risk_checks
		WHERE risk_decision_id = $1
		ORDER BY created_at`

	rows, err := r.pool.Query(ctx, query, decisionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var checks []*RiskCheck
	for rows.Next() {
		check := &RiskCheck{}
		err := rows.Scan(
			&check.ID,
			&check.RiskDecisionID,
			&check.CheckType,
			&check.Result,
			&check.ObservedValue,
			&check.ThresholdValue,
			&check.ReasonCode,
			&check.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		checks = append(checks, check)
	}
	return checks, nil
}
