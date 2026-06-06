-- URA EFRIS and banking integration persistence (adapter wiring follows).

CREATE TABLE IF NOT EXISTS efris_submissions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL DEFAULT 'default',
    document_ref TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'submitted', 'acknowledged', 'failed')),
    ura_receipt TEXT,
    error_message TEXT,
    submitted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, document_ref)
);

CREATE INDEX IF NOT EXISTS idx_efris_tenant_status ON efris_submissions (tenant_id, status);

CREATE TABLE IF NOT EXISTS bank_statements (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL DEFAULT 'default',
    bank_account_code TEXT NOT NULL,
    statement_date DATE NOT NULL,
    status TEXT NOT NULL DEFAULT 'imported' CHECK (status IN ('imported', 'reconciling', 'reconciled', 'failed')),
    line_count INT NOT NULL DEFAULT 0 CHECK (line_count >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_bank_statements_tenant ON bank_statements (tenant_id, statement_date DESC);
