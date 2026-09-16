package reconciliation

import (
	"context"
	"encoding/json"
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

func (r *Repository) CreateRun(ctx context.Context, run *ReconciliationRun) error {
	query := `
		INSERT INTO reconciliation_runs (id, tenant_id, venue_account_id, trigger_source, status, started_at, summary)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`

	_, err := r.pool.Exec(ctx, query,
		run.ID, run.TenantID, run.VenueAccountID, run.TriggerSource,
		run.Status, run.StartedAt, run.Summary,
	)
	return err
}

func (r *Repository) GetRunByID(ctx context.Context, id uuid.UUID) (*ReconciliationRun, error) {
	query := `
		SELECT id, tenant_id, venue_account_id, trigger_source, status, started_at, completed_at, summary
		FROM reconciliation_runs WHERE id = $1`

	run := &ReconciliationRun{}
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&run.ID, &run.TenantID, &run.VenueAccountID, &run.TriggerSource,
		&run.Status, &run.StartedAt, &run.CompletedAt, &run.Summary,
	)
	if err != nil {
		return nil, err
	}
	return run, nil
}

func (r *Repository) UpdateRunStatus(ctx context.Context, id uuid.UUID, status RunStatus, summary json.RawMessage) error {
	now := time.Now()
	query := `UPDATE reconciliation_runs SET status = $2, completed_at = $3, summary = $4 WHERE id = $1`
	_, err := r.pool.Exec(ctx, query, id, status, now, summary)
	return err
}

func (r *Repository) ListRunsByTenant(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]*ReconciliationRun, error) {
	query := `
		SELECT id, tenant_id, venue_account_id, trigger_source, status, started_at, completed_at, summary
		FROM reconciliation_runs WHERE tenant_id = $1
		ORDER BY started_at DESC
		LIMIT $2 OFFSET $3`

	rows, err := r.pool.Query(ctx, query, tenantID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []*ReconciliationRun
	for rows.Next() {
		run := &ReconciliationRun{}
		err := rows.Scan(
			&run.ID, &run.TenantID, &run.VenueAccountID, &run.TriggerSource,
			&run.Status, &run.StartedAt, &run.CompletedAt, &run.Summary,
		)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, nil
}

func (r *Repository) CreateItem(ctx context.Context, item *ReconciliationItem) error {
	query := `
		INSERT INTO reconciliation_items (id, run_id, entity_type, entity_id, result, internal_state, external_state, diff, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`

	_, err := r.pool.Exec(ctx, query,
		item.ID, item.RunID, item.EntityType, item.EntityID,
		item.Result, item.InternalState, item.ExternalState, item.Diff, item.CreatedAt,
	)
	return err
}

func (r *Repository) ListItemsByRun(ctx context.Context, runID uuid.UUID) ([]*ReconciliationItem, error) {
	query := `
		SELECT id, run_id, entity_type, entity_id, result, internal_state, external_state, diff, created_at
		FROM reconciliation_items WHERE run_id = $1
		ORDER BY created_at`

	rows, err := r.pool.Query(ctx, query, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []*ReconciliationItem
	for rows.Next() {
		item := &ReconciliationItem{}
		err := rows.Scan(
			&item.ID, &item.RunID, &item.EntityType, &item.EntityID,
			&item.Result, &item.InternalState, &item.ExternalState, &item.Diff, &item.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func (r *Repository) ListItemsByRunAndType(ctx context.Context, runID uuid.UUID, entityType EntityType) ([]*ReconciliationItem, error) {
	query := `
		SELECT id, run_id, entity_type, entity_id, result, internal_state, external_state, diff, created_at
		FROM reconciliation_items WHERE run_id = $1 AND entity_type = $2
		ORDER BY created_at`

	rows, err := r.pool.Query(ctx, query, runID, entityType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []*ReconciliationItem
	for rows.Next() {
		item := &ReconciliationItem{}
		err := rows.Scan(
			&item.ID, &item.RunID, &item.EntityType, &item.EntityID,
			&item.Result, &item.InternalState, &item.ExternalState, &item.Diff, &item.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func (r *Repository) CountItemsByRun(ctx context.Context, runID uuid.UUID) (int, error) {
	query := `SELECT COUNT(*) FROM reconciliation_items WHERE run_id = $1`
	var count int
	err := r.pool.QueryRow(ctx, query, runID).Scan(&count)
	return count, err
}

func (r *Repository) CountItemsByRunAndResult(ctx context.Context, runID uuid.UUID, result ItemResult) (int, error) {
	query := `SELECT COUNT(*) FROM reconciliation_items WHERE run_id = $1 AND result = $2`
	var count int
	err := r.pool.QueryRow(ctx, query, runID, result).Scan(&count)
	return count, err
}
