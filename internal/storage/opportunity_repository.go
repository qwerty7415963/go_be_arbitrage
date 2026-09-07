package storage

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type OpportunityRepository struct {
	db *pgxpool.Pool
}

func NewOpportunityRepository(db *pgxpool.Pool) *OpportunityRepository {
	return &OpportunityRepository{db: db}
}

func (r *OpportunityRepository) Create(ctx context.Context, opp *Opportunity) error {
	payloadJSON, _ := json.Marshal(opp.Payload)

	query := `
		INSERT INTO opportunities (
			tenant_id, strategy_type_id, instrument_id, opportunity_type,
			detected_at, expires_at, buy_venue_account_id, sell_venue_account_id,
			suggested_size, suggested_notional, executable_buy_price, executable_sell_price,
			gross_edge, estimated_fees, estimated_slippage, estimated_other_costs,
			expected_net_edge, expected_net_pnl, expected_holding_seconds, confidence,
			market_quality, calculation_version, market_state_ref, payload
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24)
		RETURNING id, created_at, updated_at`

	return r.db.QueryRow(ctx, query,
		opp.TenantID, opp.StrategyTypeID, opp.InstrumentID, opp.OpportunityType,
		opp.DetectedAt, opp.ExpiresAt, opp.BuyVenueAccountID, opp.SellVenueAccountID,
		opp.SuggestedSize, opp.SuggestedNotional, opp.ExecutableBuyPrice, opp.ExecutableSellPrice,
		opp.GrossEdge, opp.EstimatedFees, opp.EstimatedSlippage, opp.EstimatedOtherCosts,
		opp.ExpectedNetEdge, opp.ExpectedNetPnl, opp.ExpectedHoldingSecs, opp.Confidence,
		opp.MarketQuality, opp.CalculationVersion, opp.MarketStateRef, payloadJSON,
	).Scan(&opp.ID, &opp.CreatedAt, &opp.UpdatedAt)
}

func (r *OpportunityRepository) GetByID(ctx context.Context, id uuid.UUID) (*Opportunity, error) {
	query := `
		SELECT id, tenant_id, strategy_type_id, instrument_id, opportunity_type,
			detected_at, expires_at, buy_venue_account_id, sell_venue_account_id,
			suggested_size, suggested_notional, executable_buy_price, executable_sell_price,
			gross_edge, estimated_fees, estimated_slippage, estimated_other_costs,
			expected_net_edge, expected_net_pnl, expected_holding_seconds, confidence,
			market_quality, calculation_version, market_state_ref, payload,
			created_at, updated_at
		FROM opportunities
		WHERE id = $1`

	opp := &Opportunity{}
	err := r.db.QueryRow(ctx, query, id).Scan(
		&opp.ID, &opp.TenantID, &opp.StrategyTypeID, &opp.InstrumentID, &opp.OpportunityType,
		&opp.DetectedAt, &opp.ExpiresAt, &opp.BuyVenueAccountID, &opp.SellVenueAccountID,
		&opp.SuggestedSize, &opp.SuggestedNotional, &opp.ExecutableBuyPrice, &opp.ExecutableSellPrice,
		&opp.GrossEdge, &opp.EstimatedFees, &opp.EstimatedSlippage, &opp.EstimatedOtherCosts,
		&opp.ExpectedNetEdge, &opp.ExpectedNetPnl, &opp.ExpectedHoldingSecs, &opp.Confidence,
		&opp.MarketQuality, &opp.CalculationVersion, &opp.MarketStateRef, &opp.Payload,
		&opp.CreatedAt, &opp.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return opp, nil
}

func (r *OpportunityRepository) ListByTenant(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]*Opportunity, error) {
	query := `
		SELECT id, tenant_id, strategy_type_id, instrument_id, opportunity_type,
			detected_at, expires_at, buy_venue_account_id, sell_venue_account_id,
			suggested_size, suggested_notional, executable_buy_price, executable_sell_price,
			gross_edge, estimated_fees, estimated_slippage, estimated_other_costs,
			expected_net_edge, expected_net_pnl, expected_holding_seconds, confidence,
			market_quality, calculation_version, market_state_ref, payload,
			created_at, updated_at
		FROM opportunities
		WHERE tenant_id = $1
		ORDER BY detected_at DESC
		LIMIT $2 OFFSET $3`

	rows, err := r.db.Query(ctx, query, tenantID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var opps []*Opportunity
	for rows.Next() {
		opp := &Opportunity{}
		err := rows.Scan(
			&opp.ID, &opp.TenantID, &opp.StrategyTypeID, &opp.InstrumentID, &opp.OpportunityType,
			&opp.DetectedAt, &opp.ExpiresAt, &opp.BuyVenueAccountID, &opp.SellVenueAccountID,
			&opp.SuggestedSize, &opp.SuggestedNotional, &opp.ExecutableBuyPrice, &opp.ExecutableSellPrice,
			&opp.GrossEdge, &opp.EstimatedFees, &opp.EstimatedSlippage, &opp.EstimatedOtherCosts,
			&opp.ExpectedNetEdge, &opp.ExpectedNetPnl, &opp.ExpectedHoldingSecs, &opp.Confidence,
			&opp.MarketQuality, &opp.CalculationVersion, &opp.MarketStateRef, &opp.Payload,
			&opp.CreatedAt, &opp.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		opps = append(opps, opp)
	}
	return opps, rows.Err()
}

func (r *OpportunityRepository) DeleteExpired(ctx context.Context, before time.Time) (int64, error) {
	query := `DELETE FROM opportunities WHERE expires_at < $1`
	result, err := r.db.Exec(ctx, query, before)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

func (r *OpportunityRepository) CreateLeg(ctx context.Context, leg *OpportunityLeg) error {
	metadataJSON, _ := json.Marshal(leg.Metadata)

	query := `
		INSERT INTO opportunity_legs (
			opportunity_id, leg_role, venue_account_id, instrument_id,
			target_size, target_notional, expected_price, expected_funding, metadata
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id`

	return r.db.QueryRow(ctx, query,
		leg.OpportunityID, leg.LegRole, leg.VenueAccountID, leg.InstrumentID,
		leg.TargetSize, leg.TargetNotional, leg.ExpectedPrice, leg.ExpectedFunding, metadataJSON,
	).Scan(&leg.ID)
}

func (r *OpportunityRepository) ListLegsByOpportunity(ctx context.Context, opportunityID uuid.UUID) ([]*OpportunityLeg, error) {
	query := `
		SELECT id, opportunity_id, leg_role, venue_account_id, instrument_id,
			target_size, target_notional, expected_price, expected_funding, metadata
		FROM opportunity_legs
		WHERE opportunity_id = $1`

	rows, err := r.db.Query(ctx, query, opportunityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var legs []*OpportunityLeg
	for rows.Next() {
		leg := &OpportunityLeg{}
		err := rows.Scan(
			&leg.ID, &leg.OpportunityID, &leg.LegRole, &leg.VenueAccountID, &leg.InstrumentID,
			&leg.TargetSize, &leg.TargetNotional, &leg.ExpectedPrice, &leg.ExpectedFunding, &leg.Metadata,
		)
		if err != nil {
			return nil, err
		}
		legs = append(legs, leg)
	}
	return legs, rows.Err()
}
