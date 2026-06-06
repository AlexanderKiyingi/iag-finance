-- Bank feed lines, legacy transaction view, overdue notification tracking.

CREATE TABLE IF NOT EXISTS bank_statement_lines (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL DEFAULT 'default',
    statement_id UUID NOT NULL REFERENCES bank_statements(id) ON DELETE CASCADE,
    line_date DATE NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    payee TEXT NOT NULL DEFAULT '',
    amount NUMERIC(18, 2) NOT NULL CHECK (amount > 0),
    direction TEXT NOT NULL CHECK (direction IN ('credit', 'debit')),
    external_ref TEXT,
    match_status TEXT NOT NULL DEFAULT 'unmatched',
    matched_document_ref TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_bank_stmt_lines_stmt ON bank_statement_lines (statement_id);
CREATE INDEX IF NOT EXISTS idx_bank_stmt_lines_tenant ON bank_statement_lines (tenant_id, line_date DESC);

CREATE TABLE IF NOT EXISTS bank_transactions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL DEFAULT 'default',
    bank_account_code TEXT NOT NULL,
    txn_date DATE NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    payee TEXT NOT NULL DEFAULT '',
    category TEXT NOT NULL DEFAULT '',
    spent NUMERIC(18, 2),
    received NUMERIC(18, 2),
    action_label TEXT NOT NULL DEFAULT 'add',
    matched_ref TEXT,
    statement_line_id UUID REFERENCES bank_statement_lines(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_bank_tx_tenant ON bank_transactions (tenant_id, txn_date DESC);

ALTER TABLE ar_open_items
    ADD COLUMN IF NOT EXISTS overdue_notified_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_ar_overdue ON ar_open_items (tenant_id, due_date)
    WHERE status IN ('open', 'partial') AND due_date IS NOT NULL;
