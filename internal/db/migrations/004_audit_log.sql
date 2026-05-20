-- Accounts service audit trail and activity monitoring

CREATE TABLE IF NOT EXISTS finance_audit_log (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_type TEXT NOT NULL,
    actor_id UUID,
    actor_email TEXT NOT NULL DEFAULT '',
    resource_type TEXT,
    resource_id TEXT,
    http_method TEXT,
    http_path TEXT,
    status_code INT,
    ip_address TEXT,
    user_agent TEXT,
    correlation_id TEXT,
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_finance_audit_created ON finance_audit_log(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_finance_audit_event_type ON finance_audit_log(event_type);
CREATE INDEX IF NOT EXISTS idx_finance_audit_actor ON finance_audit_log(actor_id) WHERE actor_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_finance_audit_resource ON finance_audit_log(resource_type, resource_id)
    WHERE resource_type IS NOT NULL;
