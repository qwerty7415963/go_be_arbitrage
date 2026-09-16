package execution

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

func (r *Repository) CreateExecution(ctx context.Context, exec *Execution) error {
	query := `
		INSERT INTO executions (id, tenant_id, strategy_instance_id, opportunity_id, execution_mode, status, intent_json, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`

	_, err := r.pool.Exec(ctx, query,
		exec.ID, exec.TenantID, exec.StrategyInstanceID, exec.OpportunityID,
		exec.ExecutionMode, exec.Status, exec.IntentJSON, exec.CreatedAt,
	)
	return err
}

func (r *Repository) GetExecutionByID(ctx context.Context, id uuid.UUID) (*Execution, error) {
	query := `
		SELECT id, tenant_id, strategy_instance_id, opportunity_id, execution_mode, status, intent_json, created_at, completed_at, final_pnl
		FROM executions WHERE id = $1`

	exec := &Execution{}
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&exec.ID, &exec.TenantID, &exec.StrategyInstanceID, &exec.OpportunityID,
		&exec.ExecutionMode, &exec.Status, &exec.IntentJSON, &exec.CreatedAt,
		&exec.CompletedAt, &exec.FinalPnL,
	)
	if err != nil {
		return nil, err
	}
	return exec, nil
}

func (r *Repository) UpdateExecutionStatus(ctx context.Context, id uuid.UUID, status ExecutionStatus) error {
	query := `UPDATE executions SET status = $2 WHERE id = $1`
	_, err := r.pool.Exec(ctx, query, id, status)
	return err
}

func (r *Repository) CreateLeg(ctx context.Context, leg *ExecutionLeg) error {
	query := `
		INSERT INTO execution_legs (id, execution_id, leg_index, leg_role, venue_account_id, instrument_id, target_side, target_quantity, target_notional, status, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`

	_, err := r.pool.Exec(ctx, query,
		leg.ID, leg.ExecutionID, leg.LegIndex, leg.LegRole,
		leg.VenueAccountID, leg.InstrumentID, leg.TargetSide,
		leg.TargetQuantity, leg.TargetNotional, leg.Status, leg.Metadata,
	)
	return err
}

func (r *Repository) ListLegsByExecution(ctx context.Context, executionID uuid.UUID) ([]*ExecutionLeg, error) {
	query := `
		SELECT id, execution_id, leg_index, leg_role, venue_account_id, instrument_id, target_side, target_quantity, target_notional, status, started_at, completed_at, metadata
		FROM execution_legs WHERE execution_id = $1 ORDER BY leg_index`

	rows, err := r.pool.Query(ctx, query, executionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var legs []*ExecutionLeg
	for rows.Next() {
		leg := &ExecutionLeg{}
		err := rows.Scan(
			&leg.ID, &leg.ExecutionID, &leg.LegIndex, &leg.LegRole,
			&leg.VenueAccountID, &leg.InstrumentID, &leg.TargetSide,
			&leg.TargetQuantity, &leg.TargetNotional, &leg.Status,
			&leg.StartedAt, &leg.CompletedAt, &leg.Metadata,
		)
		if err != nil {
			return nil, err
		}
		legs = append(legs, leg)
	}
	return legs, nil
}

func (r *Repository) CreateOrder(ctx context.Context, order *Order) error {
	query := `
		INSERT INTO orders (id, execution_leg_id, venue_account_id, instrument_id, client_order_id, side, order_type, requested_quantity, requested_price, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`

	_, err := r.pool.Exec(ctx, query,
		order.ID, order.ExecutionLegID, order.VenueAccountID, order.InstrumentID,
		order.ClientOrderID, order.Side, order.OrderType,
		order.RequestedQuantity, order.RequestedPrice, order.Status,
		order.CreatedAt, order.UpdatedAt,
	)
	return err
}

func (r *Repository) GetOrderByClientOrderID(ctx context.Context, venueAccountID uuid.UUID, clientOrderID string) (*Order, error) {
	query := `
		SELECT id, execution_leg_id, venue_account_id, instrument_id, client_order_id, side, order_type, requested_quantity, requested_price, status, created_at, updated_at
		FROM orders WHERE venue_account_id = $1 AND client_order_id = $2`

	order := &Order{}
	err := r.pool.QueryRow(ctx, query, venueAccountID, clientOrderID).Scan(
		&order.ID, &order.ExecutionLegID, &order.VenueAccountID, &order.InstrumentID,
		&order.ClientOrderID, &order.Side, &order.OrderType,
		&order.RequestedQuantity, &order.RequestedPrice, &order.Status,
		&order.CreatedAt, &order.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return order, nil
}

func (r *Repository) UpdateOrderStatus(ctx context.Context, id uuid.UUID, status OrderStatus) error {
	query := `UPDATE orders SET status = $2, updated_at = $3 WHERE id = $1`
	_, err := r.pool.Exec(ctx, query, id, status, time.Now())
	return err
}

func (r *Repository) CreateFill(ctx context.Context, fill *Fill) error {
	query := `
		INSERT INTO fills (id, order_id, venue_account_id, exchange_fill_id, filled_at, quantity, price, fee_amount, fee_asset, liquidity_role)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`

	_, err := r.pool.Exec(ctx, query,
		fill.ID, fill.OrderID, fill.VenueAccountID, fill.ExchangeFillID,
		fill.FilledAt, fill.Quantity, fill.Price,
		fill.FeeAmount, fill.FeeAsset, fill.LiquidityRole,
	)
	return err
}

func (r *Repository) ListFillsByOrder(ctx context.Context, orderID uuid.UUID) ([]*Fill, error) {
	query := `
		SELECT id, order_id, venue_account_id, exchange_fill_id, filled_at, quantity, price, fee_amount, fee_asset, liquidity_role
		FROM fills WHERE order_id = $1 ORDER BY filled_at`

	rows, err := r.pool.Query(ctx, query, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var fills []*Fill
	for rows.Next() {
		fill := &Fill{}
		err := rows.Scan(
			&fill.ID, &fill.OrderID, &fill.VenueAccountID, &fill.ExchangeFillID,
			&fill.FilledAt, &fill.Quantity, &fill.Price,
			&fill.FeeAmount, &fill.FeeAsset, &fill.LiquidityRole,
		)
		if err != nil {
			return nil, err
		}
		fills = append(fills, fill)
	}
	return fills, nil
}
