package storage

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type DecisionRepository struct {
	db *pgxpool.Pool
}

func NewDecisionRepository(db *pgxpool.Pool) *DecisionRepository {
	return &DecisionRepository{db: db}
}

func (r *DecisionRepository) CreateStrategyDecision(ctx context.Context, decision *StrategyDecision) error {
	reasonDetailJSON, _ := json.Marshal(decision.ReasonDetail)

	query := `
		INSERT INTO strategy_decisions (
			opportunity_id, strategy_instance_id, decision, reason_code,
			reason_detail, config_version_id, decision_timestamp, input_market_state_ref
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id`

	return r.db.QueryRow(ctx, query,
		decision.OpportunityID, decision.StrategyInstanceID, decision.Decision,
		decision.ReasonCode, reasonDetailJSON, decision.ConfigVersionID,
		decision.DecisionTimestamp, decision.InputMarketStateRef,
	).Scan(&decision.ID)
}

func (r *DecisionRepository) GetStrategyDecision(ctx context.Context, id uuid.UUID) (*StrategyDecision, error) {
	query := `
		SELECT id, opportunity_id, strategy_instance_id, decision, reason_code,
			reason_detail, config_version_id, decision_timestamp, input_market_state_ref
		FROM strategy_decisions
		WHERE id = $1`

	decision := &StrategyDecision{}
	var reasonDetail []byte

	err := r.db.QueryRow(ctx, query, id).Scan(
		&decision.ID, &decision.OpportunityID, &decision.StrategyInstanceID,
		&decision.Decision, &decision.ReasonCode, &reasonDetail,
		&decision.ConfigVersionID, &decision.DecisionTimestamp, &decision.InputMarketStateRef,
	)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(reasonDetail, &decision.ReasonDetail); err != nil {
		return nil, err
	}

	return decision, nil
}

func (r *DecisionRepository) ListStrategyDecisionsByInstance(ctx context.Context, instanceID uuid.UUID, limit, offset int) ([]*StrategyDecision, error) {
	query := `
		SELECT id, opportunity_id, strategy_instance_id, decision, reason_code,
			reason_detail, config_version_id, decision_timestamp, input_market_state_ref
		FROM strategy_decisions
		WHERE strategy_instance_id = $1
		ORDER BY decision_timestamp DESC
		LIMIT $2 OFFSET $3`

	rows, err := r.db.Query(ctx, query, instanceID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var decisions []*StrategyDecision
	for rows.Next() {
		d := &StrategyDecision{}
		var reasonDetail []byte

		err := rows.Scan(
			&d.ID, &d.OpportunityID, &d.StrategyInstanceID,
			&d.Decision, &d.ReasonCode, &reasonDetail,
			&d.ConfigVersionID, &d.DecisionTimestamp, &d.InputMarketStateRef,
		)
		if err != nil {
			return nil, err
		}

		if err := json.Unmarshal(reasonDetail, &d.ReasonDetail); err != nil {
			return nil, err
		}

		decisions = append(decisions, d)
	}
	return decisions, rows.Err()
}

func (r *DecisionRepository) CreateRiskDecision(ctx context.Context, decision *RiskDecision) error {
	reasonDetailJSON, _ := json.Marshal(decision.ReasonDetail)

	query := `
		INSERT INTO risk_decisions (
			execution_id, opportunity_id, strategy_instance_id, decision,
			reason_code, reason_detail, risk_policy_version_id, evaluated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id`

	return r.db.QueryRow(ctx, query,
		decision.ExecutionID, decision.OpportunityID, decision.StrategyInstanceID,
		decision.Decision, decision.ReasonCode, reasonDetailJSON,
		decision.RiskPolicyVersionID, decision.EvaluatedAt,
	).Scan(&decision.ID)
}

func (r *DecisionRepository) GetRiskDecision(ctx context.Context, id uuid.UUID) (*RiskDecision, error) {
	query := `
		SELECT id, execution_id, opportunity_id, strategy_instance_id, decision,
			reason_code, reason_detail, risk_policy_version_id, evaluated_at
		FROM risk_decisions
		WHERE id = $1`

	decision := &RiskDecision{}
	var reasonDetail []byte

	err := r.db.QueryRow(ctx, query, id).Scan(
		&decision.ID, &decision.ExecutionID, &decision.OpportunityID,
		&decision.StrategyInstanceID, &decision.Decision, &decision.ReasonCode,
		&reasonDetail, &decision.RiskPolicyVersionID, &decision.EvaluatedAt,
	)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(reasonDetail, &decision.ReasonDetail); err != nil {
		return nil, err
	}

	return decision, nil
}

func (r *DecisionRepository) ListRiskDecisionsByInstance(ctx context.Context, instanceID uuid.UUID, limit, offset int) ([]*RiskDecision, error) {
	query := `
		SELECT id, execution_id, opportunity_id, strategy_instance_id, decision,
			reason_code, reason_detail, risk_policy_version_id, evaluated_at
		FROM risk_decisions
		WHERE strategy_instance_id = $1
		ORDER BY evaluated_at DESC
		LIMIT $2 OFFSET $3`

	rows, err := r.db.Query(ctx, query, instanceID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var decisions []*RiskDecision
	for rows.Next() {
		d := &RiskDecision{}
		var reasonDetail []byte

		err := rows.Scan(
			&d.ID, &d.ExecutionID, &d.OpportunityID,
			&d.StrategyInstanceID, &d.Decision, &d.ReasonCode,
			&reasonDetail, &d.RiskPolicyVersionID, &d.EvaluatedAt,
		)
		if err != nil {
			return nil, err
		}

		if err := json.Unmarshal(reasonDetail, &d.ReasonDetail); err != nil {
			return nil, err
		}

		decisions = append(decisions, d)
	}
	return decisions, rows.Err()
}
