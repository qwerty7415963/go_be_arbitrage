-- Phase 5: Opportunities, Decisions, and Unified States

-- Opportunities
CREATE TABLE IF NOT EXISTS opportunities (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    strategy_type_id UUID NOT NULL REFERENCES strategy_types(id),
    instrument_id UUID NOT NULL REFERENCES instruments(id),
    opportunity_type TEXT NOT NULL CHECK (opportunity_type IN ('PRICE_ARBITRAGE', 'FUNDING_ARBITRAGE', 'BASIS_ARBITRAGE')),
    detected_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL,
    buy_venue_account_id UUID REFERENCES venue_accounts(id),
    sell_venue_account_id UUID REFERENCES venue_accounts(id),
    suggested_size NUMERIC(30,12),
    suggested_notional NUMERIC(30,12),
    executable_buy_price NUMERIC(30,12),
    executable_sell_price NUMERIC(30,12),
    gross_edge NUMERIC(24,12),
    estimated_fees NUMERIC(30,12),
    estimated_slippage NUMERIC(30,12),
    estimated_other_costs NUMERIC(30,12),
    expected_net_edge NUMERIC(24,12),
    expected_net_pnl NUMERIC(30,12),
    expected_holding_seconds INTEGER,
    confidence NUMERIC(8,6),
    market_quality TEXT NOT NULL DEFAULT 'GOOD' CHECK (market_quality IN ('GOOD', 'DEGRADED', 'UNUSABLE')),
    calculation_version TEXT NOT NULL,
    market_state_ref TEXT,
    payload JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Opportunity Legs
CREATE TABLE IF NOT EXISTS opportunity_legs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    opportunity_id UUID NOT NULL REFERENCES opportunities(id) ON DELETE CASCADE,
    leg_role TEXT NOT NULL CHECK (leg_role IN ('BUY', 'SELL', 'LONG', 'SHORT', 'SPOT', 'PERP', 'HEDGE')),
    venue_account_id UUID NOT NULL REFERENCES venue_accounts(id),
    instrument_id UUID NOT NULL REFERENCES instruments(id),
    target_size NUMERIC(30,12),
    target_notional NUMERIC(30,12),
    expected_price NUMERIC(30,12),
    expected_funding NUMERIC(24,12),
    metadata JSONB NOT NULL DEFAULT '{}'
);

-- Strategy Decisions
CREATE TABLE IF NOT EXISTS strategy_decisions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    opportunity_id UUID REFERENCES opportunities(id),
    strategy_instance_id UUID NOT NULL REFERENCES strategy_instances(id),
    decision TEXT NOT NULL CHECK (decision IN ('ACCEPT', 'REJECT', 'IGNORE', 'PAUSE')),
    reason_code TEXT NOT NULL,
    reason_detail JSONB NOT NULL DEFAULT '{}',
    config_version_id UUID REFERENCES config_versions(id),
    decision_timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    input_market_state_ref TEXT
);

-- Risk Decisions
CREATE TABLE IF NOT EXISTS risk_decisions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    execution_id UUID,
    opportunity_id UUID REFERENCES opportunities(id),
    strategy_instance_id UUID NOT NULL REFERENCES strategy_instances(id),
    decision TEXT NOT NULL CHECK (decision IN ('ALLOW', 'REJECT', 'RESTRICT', 'HEDGE_REQUIRED', 'HALT')),
    reason_code TEXT NOT NULL,
    reason_detail JSONB NOT NULL DEFAULT '{}',
    risk_policy_version_id UUID REFERENCES config_versions(id),
    evaluated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Unified Instrument States
CREATE TABLE IF NOT EXISTS unified_instrument_states (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    instrument_id UUID NOT NULL REFERENCES instruments(id) UNIQUE,
    canonical_symbol TEXT NOT NULL,
    best_bid NUMERIC(30,12),
    best_ask NUMERIC(30,12),
    spread NUMERIC(24,12),
    last_price NUMERIC(30,12),
    bid_depth JSONB NOT NULL DEFAULT '[]',
    ask_depth JSONB NOT NULL DEFAULT '[]',
    funding JSONB,
    mark_price NUMERIC(30,12),
    index_price NUMERIC(30,12),
    healthy_venues INTEGER NOT NULL DEFAULT 0,
    total_venues INTEGER NOT NULL DEFAULT 0,
    is_stale BOOLEAN NOT NULL DEFAULT FALSE,
    last_update TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- System Events
CREATE TABLE IF NOT EXISTS system_events (
    id BIGSERIAL PRIMARY KEY,
    tenant_id UUID REFERENCES tenants(id),
    event_type TEXT NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    correlation_id UUID,
    payload JSONB NOT NULL
);

-- Indexes for new tables
CREATE INDEX IF NOT EXISTS idx_opportunities_tenant_time ON opportunities(tenant_id, detected_at DESC);
CREATE INDEX IF NOT EXISTS idx_opportunities_strategy_time ON opportunities(strategy_type_id, expires_at);
CREATE INDEX IF NOT EXISTS idx_opportunities_instrument_time ON opportunities(instrument_id, detected_at DESC);
CREATE INDEX IF NOT EXISTS idx_opportunity_legs_opportunity ON opportunity_legs(opportunity_id);
CREATE INDEX IF NOT EXISTS idx_strategy_decisions_instance_time ON strategy_decisions(strategy_instance_id, decision_timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_risk_decisions_strategy_time ON risk_decisions(strategy_instance_id, evaluated_at DESC);
CREATE INDEX IF NOT EXISTS idx_system_events_tenant_time ON system_events(tenant_id, occurred_at DESC);
