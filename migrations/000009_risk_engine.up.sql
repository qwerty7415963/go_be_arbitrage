-- Risk Policies
CREATE TABLE IF NOT EXISTS risk_policies (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    name TEXT NOT NULL,
    policy_type TEXT NOT NULL CHECK (policy_type IN ('GLOBAL', 'STRATEGY', 'VENUE', 'ACCOUNT', 'INSTRUMENT')),
    config JSONB NOT NULL DEFAULT '{}',
    status TEXT NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'DISABLED')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(tenant_id, name)
);

-- Risk Checks
CREATE TABLE IF NOT EXISTS risk_checks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    risk_decision_id UUID REFERENCES risk_decisions(id),
    check_type TEXT NOT NULL,
    result TEXT NOT NULL CHECK (result IN ('PASS', 'FAIL', 'WARN', 'SKIP')),
    observed_value JSONB NOT NULL DEFAULT '{}',
    threshold_value JSONB,
    reason_code TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_risk_checks_decision ON risk_checks(risk_decision_id);
CREATE INDEX IF NOT EXISTS idx_risk_policies_tenant ON risk_policies(tenant_id, status);
