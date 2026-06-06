-- Payment application against AR/AP open items.

ALTER TABLE ar_open_items
    ADD COLUMN IF NOT EXISTS amount_paid NUMERIC(18, 2) NOT NULL DEFAULT 0 CHECK (amount_paid >= 0);

ALTER TABLE ap_open_items
    ADD COLUMN IF NOT EXISTS amount_paid NUMERIC(18, 2) NOT NULL DEFAULT 0 CHECK (amount_paid >= 0);

-- Per-tenant document uniqueness (was global on document_ref alone).
ALTER TABLE ar_open_items DROP CONSTRAINT IF EXISTS ar_open_items_document_ref_key;
ALTER TABLE ap_open_items DROP CONSTRAINT IF EXISTS ap_open_items_document_ref_key;
CREATE UNIQUE INDEX IF NOT EXISTS idx_ar_document_tenant ON ar_open_items (tenant_id, document_ref);
CREATE UNIQUE INDEX IF NOT EXISTS idx_ap_document_tenant ON ap_open_items (tenant_id, document_ref);

CREATE TABLE IF NOT EXISTS finance_payments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL DEFAULT 'default',
    direction TEXT NOT NULL CHECK (direction IN ('ar', 'ap')),
    open_item_id UUID NOT NULL,
    amount NUMERIC(18, 2) NOT NULL CHECK (amount > 0),
    currency TEXT NOT NULL DEFAULT 'UGX',
    payment_ref TEXT NOT NULL DEFAULT '',
    journal_entry_id UUID REFERENCES journal_entries(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_finance_payments_item ON finance_payments (open_item_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_finance_payments_tenant ON finance_payments (tenant_id, created_at DESC);
