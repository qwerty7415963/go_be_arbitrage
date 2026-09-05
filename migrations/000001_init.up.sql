-- Initial schema for Arbitrage Platform

-- Tenants
CREATE TABLE IF NOT EXISTS tenants (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'SUSPENDED')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Users
CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    email TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'DISABLED')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_login_at TIMESTAMPTZ,
    UNIQUE(tenant_id, email)
);

-- Venues
CREATE TABLE IF NOT EXISTS venues (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code TEXT UNIQUE NOT NULL,
    name TEXT NOT NULL,
    venue_type TEXT NOT NULL CHECK (venue_type IN ('CEX', 'PERP_DEX')),
    status TEXT NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'DISABLED')),
    capabilities JSONB NOT NULL DEFAULT '{}',
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Venue Accounts
CREATE TABLE IF NOT EXISTS venue_accounts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    owner_user_id UUID REFERENCES users(id),
    venue_id UUID NOT NULL REFERENCES venues(id),
    account_name TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'DISABLED', 'DEGRADED', 'RECONCILIATION_REQUIRED')),
    external_account_ref TEXT,
    environment TEXT NOT NULL DEFAULT 'MAINNET' CHECK (environment IN ('MAINNET', 'TESTNET', 'SANDBOX')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(tenant_id, venue_id, account_name)
);

-- Instruments
CREATE TABLE IF NOT EXISTS instruments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    canonical_symbol TEXT UNIQUE NOT NULL,
    base_asset TEXT NOT NULL,
    quote_asset TEXT NOT NULL,
    instrument_type TEXT NOT NULL CHECK (instrument_type IN ('SPOT', 'PERP', 'FUTURE')),
    contract_type TEXT NOT NULL DEFAULT 'OTHER' CHECK (contract_type IN ('LINEAR', 'INVERSE', 'SPOT', 'OTHER')),
    contract_size NUMERIC(30,12),
    price_tick NUMERIC(30,12) NOT NULL,
    quantity_step NUMERIC(30,12) NOT NULL,
    min_quantity NUMERIC(30,12),
    min_notional NUMERIC(30,12),
    margin_asset TEXT,
    settlement_asset TEXT,
    discovery_status TEXT NOT NULL DEFAULT 'DISCOVERED' CHECK (discovery_status IN ('DISCOVERED', 'REVIEWED', 'REJECTED')),
    trading_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Venue Instruments
CREATE TABLE IF NOT EXISTS venue_instruments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    venue_id UUID NOT NULL REFERENCES venues(id),
    instrument_id UUID NOT NULL REFERENCES instruments(id),
    venue_symbol TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'DELISTED', 'DISABLED')),
    venue_metadata JSONB NOT NULL DEFAULT '{}',
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(venue_id, venue_symbol),
    UNIQUE(venue_id, instrument_id)
);

-- Strategy Types
CREATE TABLE IF NOT EXISTS strategy_types (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code TEXT UNIQUE NOT NULL,
    name TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'DISABLED')),
    default_config_version_id UUID
);

-- Strategy Instances
CREATE TABLE IF NOT EXISTS strategy_instances (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    strategy_type_id UUID NOT NULL REFERENCES strategy_types(id),
    name TEXT NOT NULL,
    mode TEXT NOT NULL DEFAULT 'PAPER' CHECK (mode IN ('PAPER', 'LIVE_MANUAL', 'LIVE_AUTO')),
    status TEXT NOT NULL DEFAULT 'DRAFT' CHECK (status IN ('DRAFT', 'RUNNING', 'PAUSED', 'DISABLED', 'ERROR')),
    preferred_execution_policy TEXT,
    created_by UUID REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(tenant_id, name)
);

-- Config Versions
CREATE TABLE IF NOT EXISTS config_versions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID,
    scope_type TEXT NOT NULL CHECK (scope_type IN ('GLOBAL', 'STRATEGY_TYPE', 'STRATEGY_INSTANCE', 'RISK_POLICY')),
    scope_id UUID,
    version_no BIGINT NOT NULL,
    config_json JSONB NOT NULL,
    status TEXT NOT NULL DEFAULT 'DRAFT' CHECK (status IN ('DRAFT', 'ACTIVE', 'SUPERSEDED')),
    created_by UUID REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    activated_at TIMESTAMPTZ,
    UNIQUE(tenant_id, scope_type, scope_id, version_no)
);

-- Idempotency Keys
CREATE TABLE IF NOT EXISTS idempotency_keys (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    actor_user_id UUID REFERENCES users(id),
    scope TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    request_hash TEXT NOT NULL,
    response_code INTEGER,
    response_body JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL,
    UNIQUE(tenant_id, scope, idempotency_key)
);

-- Audit Events
CREATE TABLE IF NOT EXISTS audit_events (
    id BIGSERIAL PRIMARY KEY,
    tenant_id UUID REFERENCES tenants(id),
    actor_type TEXT NOT NULL CHECK (actor_type IN ('USER', 'SYSTEM', 'STRATEGY', 'RISK', 'EXECUTION')),
    actor_id TEXT,
    action TEXT NOT NULL,
    entity_type TEXT,
    entity_id TEXT,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    correlation_id UUID,
    payload JSONB NOT NULL
);

-- Indexes
CREATE INDEX IF NOT EXISTS idx_users_tenant_status ON users(tenant_id, status);
CREATE INDEX IF NOT EXISTS idx_venue_accounts_tenant_venue_status ON venue_accounts(tenant_id, venue_id, status);
CREATE INDEX IF NOT EXISTS idx_instruments_type ON instruments(instrument_type);
CREATE INDEX IF NOT EXISTS idx_strategy_instances_tenant_status ON strategy_instances(tenant_id, status);
CREATE INDEX IF NOT EXISTS idx_config_versions_scope ON config_versions(scope_type, scope_id, status);
CREATE INDEX IF NOT EXISTS idx_audit_events_tenant_time ON audit_events(tenant_id, occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_events_entity ON audit_events(entity_type, entity_id, occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_idempotency_keys_lookup ON idempotency_keys(tenant_id, scope, idempotency_key);
