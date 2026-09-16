-- Reconciliation Runs
CREATE TABLE IF NOT EXISTS reconciliation_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    venue_account_id UUID NOT NULL REFERENCES venue_accounts(id),
    trigger_source TEXT NOT NULL CHECK (trigger_source IN ('STARTUP', 'RECONNECT', 'SCHEDULED', 'MANUAL', 'EXECUTION_RECOVERY')),
    status TEXT NOT NULL DEFAULT 'RUNNING' CHECK (status IN ('RUNNING', 'MATCHED', 'MISMATCH', 'FAILED')),
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,
    summary JSONB NOT NULL DEFAULT '{}'
);

-- Reconciliation Items
CREATE TABLE IF NOT EXISTS reconciliation_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id UUID NOT NULL REFERENCES reconciliation_runs(id) ON DELETE CASCADE,
    entity_type TEXT NOT NULL CHECK (entity_type IN ('BALANCE', 'POSITION', 'ORDER', 'FILL', 'MARGIN')),
    entity_id UUID,
    result TEXT NOT NULL CHECK (result IN ('MATCH', 'MISMATCH', 'MISSING_INTERNAL', 'MISSING_EXTERNAL', 'UNRESOLVED')),
    internal_state JSONB NOT NULL DEFAULT '{}',
    external_state JSONB NOT NULL DEFAULT '{}',
    diff JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_recon_runs_tenant ON reconciliation_runs(tenant_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_recon_items_run ON reconciliation_items(run_id);
