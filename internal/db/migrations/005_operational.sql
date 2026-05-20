-- Operational finance entities (replaces prototype HTML table_rows for inbox UIs).

CREATE TABLE IF NOT EXISTS bank_accounts (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    TEXT NOT NULL DEFAULT 'default',
    code         TEXT NOT NULL,
    name         TEXT NOT NULL,
    institution  TEXT NOT NULL DEFAULT '',
    currency     TEXT NOT NULL DEFAULT 'UGX',
    balance      NUMERIC(18, 2) NOT NULL DEFAULT 0,
    status_label TEXT NOT NULL DEFAULT 'ACTIVE',
    purpose      TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, code)
);

CREATE INDEX IF NOT EXISTS idx_bank_accounts_tenant ON bank_accounts (tenant_id);

CREATE TABLE IF NOT EXISTS cherry_intake_lines (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    TEXT NOT NULL DEFAULT 'default',
    intake_code  TEXT NOT NULL,
    farmer_name  TEXT NOT NULL,
    qty_kg       NUMERIC(12, 2) NOT NULL DEFAULT 0,
    amount_ugx   NUMERIC(18, 2) NOT NULL DEFAULT 0,
    status_label TEXT NOT NULL DEFAULT 'PENDING',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, intake_code)
);

CREATE INDEX IF NOT EXISTS idx_cherry_intake_tenant ON cherry_intake_lines (tenant_id, status_label);
