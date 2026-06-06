-- Single-organisation deployment: remove tenant_id scoping from finance tables.

-- audit_events
DROP INDEX IF EXISTS idx_audit_tenant_time;
ALTER TABLE audit_events DROP COLUMN IF EXISTS tenant_id;
CREATE INDEX IF NOT EXISTS idx_audit_occurred_at ON audit_events (occurred_at DESC);

-- table_rows
DROP INDEX IF EXISTS idx_table_rows_lookup;
ALTER TABLE table_rows DROP COLUMN IF EXISTS tenant_id;
CREATE INDEX IF NOT EXISTS idx_table_rows_lookup ON table_rows (table_id, id);

-- bank_accounts
ALTER TABLE bank_accounts DROP CONSTRAINT IF EXISTS bank_accounts_tenant_id_code_key;
DROP INDEX IF EXISTS idx_bank_accounts_tenant;
ALTER TABLE bank_accounts DROP COLUMN IF EXISTS tenant_id;
CREATE UNIQUE INDEX IF NOT EXISTS idx_bank_accounts_code ON bank_accounts (code);

-- cherry_intake_lines
ALTER TABLE cherry_intake_lines DROP CONSTRAINT IF EXISTS cherry_intake_lines_tenant_id_intake_code_key;
DROP INDEX IF EXISTS idx_cherry_intake_tenant;
ALTER TABLE cherry_intake_lines DROP COLUMN IF EXISTS tenant_id;
CREATE UNIQUE INDEX IF NOT EXISTS idx_cherry_intake_code ON cherry_intake_lines (intake_code);

-- ar_open_items
DROP INDEX IF EXISTS idx_ar_tenant;
DROP INDEX IF EXISTS idx_ar_document_tenant;
DROP INDEX IF EXISTS idx_ar_overdue;
ALTER TABLE ar_open_items DROP COLUMN IF EXISTS tenant_id;
CREATE UNIQUE INDEX IF NOT EXISTS idx_ar_document_ref ON ar_open_items (document_ref);
CREATE INDEX IF NOT EXISTS idx_ar_overdue ON ar_open_items (due_date)
    WHERE status IN ('open', 'partial') AND due_date IS NOT NULL;

-- ap_open_items
DROP INDEX IF EXISTS idx_ap_tenant;
DROP INDEX IF EXISTS idx_ap_document_tenant;
ALTER TABLE ap_open_items DROP COLUMN IF EXISTS tenant_id;
CREATE UNIQUE INDEX IF NOT EXISTS idx_ap_document_ref ON ap_open_items (document_ref);

-- finance_payments
DROP INDEX IF EXISTS idx_finance_payments_tenant;
ALTER TABLE finance_payments DROP COLUMN IF EXISTS tenant_id;

-- efris_submissions
ALTER TABLE efris_submissions DROP CONSTRAINT IF EXISTS efris_submissions_tenant_id_document_ref_key;
DROP INDEX IF EXISTS idx_efris_tenant_status;
ALTER TABLE efris_submissions DROP COLUMN IF EXISTS tenant_id;
CREATE UNIQUE INDEX IF NOT EXISTS idx_efris_document_ref ON efris_submissions (document_ref);
CREATE INDEX IF NOT EXISTS idx_efris_status ON efris_submissions (status);

-- bank_statements
DROP INDEX IF EXISTS idx_bank_statements_tenant;
ALTER TABLE bank_statements DROP COLUMN IF EXISTS tenant_id;

-- bank_statement_lines
DROP INDEX IF EXISTS idx_bank_stmt_lines_tenant;
ALTER TABLE bank_statement_lines DROP COLUMN IF EXISTS tenant_id;

-- bank_transactions
DROP INDEX IF EXISTS idx_bank_tx_tenant;
ALTER TABLE bank_transactions DROP COLUMN IF EXISTS tenant_id;
