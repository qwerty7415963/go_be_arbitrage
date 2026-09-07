package storage

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type AuditRepository struct {
	db *pgxpool.Pool
}

func NewAuditRepository(db *pgxpool.Pool) *AuditRepository {
	return &AuditRepository{db: db}
}

func (r *AuditRepository) CreateAuditEvent(ctx context.Context, event *AuditEvent) error {
	payloadJSON, _ := json.Marshal(event.Payload)

	query := `
		INSERT INTO audit_events (
			tenant_id, actor_type, actor_id, action, entity_type,
			entity_id, occurred_at, correlation_id, payload
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id`

	return r.db.QueryRow(ctx, query,
		event.TenantID, event.ActorType, event.ActorID, event.Action,
		event.EntityType, event.EntityID, event.OccurredAt,
		event.CorrelationID, payloadJSON,
	).Scan(&event.ID)
}

func (r *AuditRepository) GetAuditEvent(ctx context.Context, id int64) (*AuditEvent, error) {
	query := `
		SELECT id, tenant_id, actor_type, actor_id, action, entity_type,
			entity_id, occurred_at, correlation_id, payload
		FROM audit_events
		WHERE id = $1`

	event := &AuditEvent{}
	var payload []byte

	err := r.db.QueryRow(ctx, query, id).Scan(
		&event.ID, &event.TenantID, &event.ActorType, &event.ActorID,
		&event.Action, &event.EntityType, &event.EntityID,
		&event.OccurredAt, &event.CorrelationID, &payload,
	)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(payload, &event.Payload); err != nil {
		return nil, err
	}

	return event, nil
}

func (r *AuditRepository) ListAuditEventsByTenant(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]*AuditEvent, error) {
	query := `
		SELECT id, tenant_id, actor_type, actor_id, action, entity_type,
			entity_id, occurred_at, correlation_id, payload
		FROM audit_events
		WHERE tenant_id = $1
		ORDER BY occurred_at DESC
		LIMIT $2 OFFSET $3`

	rows, err := r.db.Query(ctx, query, tenantID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []*AuditEvent
	for rows.Next() {
		event := &AuditEvent{}
		var payload []byte

		err := rows.Scan(
			&event.ID, &event.TenantID, &event.ActorType, &event.ActorID,
			&event.Action, &event.EntityType, &event.EntityID,
			&event.OccurredAt, &event.CorrelationID, &payload,
		)
		if err != nil {
			return nil, err
		}

		if err := json.Unmarshal(payload, &event.Payload); err != nil {
			return nil, err
		}

		events = append(events, event)
	}
	return events, rows.Err()
}

func (r *AuditRepository) ListAuditEventsByEntity(ctx context.Context, entityType, entityID string, limit, offset int) ([]*AuditEvent, error) {
	query := `
		SELECT id, tenant_id, actor_type, actor_id, action, entity_type,
			entity_id, occurred_at, correlation_id, payload
		FROM audit_events
		WHERE entity_type = $1 AND entity_id = $2
		ORDER BY occurred_at DESC
		LIMIT $3 OFFSET $4`

	rows, err := r.db.Query(ctx, query, entityType, entityID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []*AuditEvent
	for rows.Next() {
		event := &AuditEvent{}
		var payload []byte

		err := rows.Scan(
			&event.ID, &event.TenantID, &event.ActorType, &event.ActorID,
			&event.Action, &event.EntityType, &event.EntityID,
			&event.OccurredAt, &event.CorrelationID, &payload,
		)
		if err != nil {
			return nil, err
		}

		if err := json.Unmarshal(payload, &event.Payload); err != nil {
			return nil, err
		}

		events = append(events, event)
	}
	return events, rows.Err()
}

func (r *AuditRepository) CreateSystemEvent(ctx context.Context, event *SystemEvent) error {
	payloadJSON, _ := json.Marshal(event.Payload)

	query := `
		INSERT INTO system_events (
			tenant_id, event_type, occurred_at, correlation_id, payload
		) VALUES ($1, $2, $3, $4, $5)
		RETURNING id`

	return r.db.QueryRow(ctx, query,
		event.TenantID, event.EventType, event.OccurredAt,
		event.CorrelationID, payloadJSON,
	).Scan(&event.ID)
}

func (r *AuditRepository) ListSystemEvents(ctx context.Context, tenantID *uuid.UUID, limit, offset int) ([]*SystemEvent, error) {
	var query string
	var args []interface{}

	if tenantID != nil {
		query = `
			SELECT id, tenant_id, event_type, occurred_at, correlation_id, payload
			FROM system_events
			WHERE tenant_id = $1
			ORDER BY occurred_at DESC
			LIMIT $2 OFFSET $3`
		args = []interface{}{*tenantID, limit, offset}
	} else {
		query = `
			SELECT id, tenant_id, event_type, occurred_at, correlation_id, payload
			FROM system_events
			ORDER BY occurred_at DESC
			LIMIT $1 OFFSET $2`
		args = []interface{}{limit, offset}
	}

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []*SystemEvent
	for rows.Next() {
		event := &SystemEvent{}
		var payload []byte

		err := rows.Scan(
			&event.ID, &event.TenantID, &event.EventType,
			&event.OccurredAt, &event.CorrelationID, &payload,
		)
		if err != nil {
			return nil, err
		}

		if err := json.Unmarshal(payload, &event.Payload); err != nil {
			return nil, err
		}

		events = append(events, event)
	}
	return events, rows.Err()
}
