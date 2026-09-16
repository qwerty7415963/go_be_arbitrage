-- Executions
CREATE TABLE IF NOT EXISTS executions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    strategy_instance_id UUID NOT NULL REFERENCES strategy_instances(id),
    opportunity_id UUID,
    execution_mode TEXT NOT NULL CHECK (execution_mode IN ('PAPER', 'LIVE_MANUAL', 'LIVE_AUTO')),
    status TEXT NOT NULL DEFAULT 'CREATED' CHECK (status IN (
        'CREATED', 'RISK_APPROVED', 'AWAITING_APPROVAL', 'PLANNED',
        'SUBMITTING', 'PARTIAL', 'COMPLETED', 'RECOVERY', 'FAILED', 'CANCELED'
    )),
    intent_json JSONB NOT NULL DEFAULT '{}',
    execution_plan_json JSONB,
    risk_decision_id UUID REFERENCES risk_decisions(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,
    final_pnl NUMERIC(30,12)
);

-- Execution Legs
CREATE TABLE IF NOT EXISTS execution_legs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    execution_id UUID NOT NULL REFERENCES executions(id) ON DELETE CASCADE,
    leg_index INTEGER NOT NULL,
    leg_role TEXT NOT NULL,
    venue_account_id UUID NOT NULL REFERENCES venue_accounts(id),
    instrument_id UUID NOT NULL REFERENCES instruments(id),
    target_side TEXT NOT NULL CHECK (target_side IN ('BUY', 'SELL')),
    target_quantity NUMERIC(30,12),
    target_notional NUMERIC(30,12),
    status TEXT NOT NULL DEFAULT 'PENDING' CHECK (status IN (
        'PENDING', 'SUBMITTING', 'OPEN', 'PARTIAL', 'FILLED', 'FAILED', 'CANCELED', 'RECOVERY'
    )),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    metadata JSONB NOT NULL DEFAULT '{}',
    UNIQUE(execution_id, leg_index)
);

-- Orders
CREATE TABLE IF NOT EXISTS orders (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    execution_leg_id UUID REFERENCES execution_legs(id),
    venue_account_id UUID NOT NULL REFERENCES venue_accounts(id),
    instrument_id UUID NOT NULL REFERENCES instruments(id),
    client_order_id TEXT NOT NULL,
    exchange_order_id TEXT,
    side TEXT NOT NULL CHECK (side IN ('BUY', 'SELL')),
    order_type TEXT NOT NULL,
    time_in_force TEXT,
    requested_quantity NUMERIC(30,12),
    requested_price NUMERIC(30,12),
    status TEXT NOT NULL DEFAULT 'CREATED' CHECK (status IN (
        'CREATED', 'SUBMITTING', 'OPEN', 'PARTIALLY_FILLED', 'FILLED',
        'CANCELING', 'CANCELED', 'REJECTED', 'EXPIRED', 'UNKNOWN'
    )),
    submit_timestamp TIMESTAMPTZ,
    ack_timestamp TIMESTAMPTZ,
    cancel_timestamp TIMESTAMPTZ,
    last_exchange_update_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_error_code TEXT,
    UNIQUE(venue_account_id, client_order_id)
);

-- Fills
CREATE TABLE IF NOT EXISTS fills (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id UUID NOT NULL REFERENCES orders(id),
    venue_account_id UUID NOT NULL REFERENCES venue_accounts(id),
    exchange_fill_id TEXT,
    filled_at TIMESTAMPTZ NOT NULL,
    quantity NUMERIC(30,12) NOT NULL,
    price NUMERIC(30,12) NOT NULL,
    fee_amount NUMERIC(30,12),
    fee_asset TEXT,
    liquidity_role TEXT,
    UNIQUE(venue_account_id, exchange_fill_id)
);

CREATE INDEX IF NOT EXISTS idx_executions_tenant ON executions(tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_executions_strategy ON executions(strategy_instance_id, status);
CREATE INDEX IF NOT EXISTS idx_execution_legs_exec ON execution_legs(execution_id);
CREATE INDEX IF NOT EXISTS idx_orders_venue_status ON orders(venue_account_id, status);
CREATE INDEX IF NOT EXISTS idx_orders_exchange_id ON orders(exchange_order_id);
CREATE INDEX IF NOT EXISTS idx_fills_order ON fills(order_id);
