-- Credit/debit notes, billing identity linkage, and customer payment links.

CREATE TABLE IF NOT EXISTS finance_adjustments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    kind TEXT NOT NULL CHECK (kind IN ('credit_note', 'debit_note')),
    direction TEXT NOT NULL CHECK (direction IN ('ar', 'ap')),
    original_document_ref TEXT NOT NULL,
    document_ref TEXT NOT NULL UNIQUE,
    party_ref TEXT NOT NULL DEFAULT '',
    amount NUMERIC(18, 2) NOT NULL CHECK (amount > 0),
    currency TEXT NOT NULL DEFAULT 'UGX',
    reason TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'posted',
    journal_entry_id UUID REFERENCES journal_entries(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_finance_adjustments_original ON finance_adjustments (original_document_ref);
CREATE INDEX IF NOT EXISTS idx_finance_adjustments_party ON finance_adjustments (party_ref);

ALTER TABLE ar_open_items
    ADD COLUMN IF NOT EXISTS billing_org_id UUID,
    ADD COLUMN IF NOT EXISTS billing_identity_id UUID,
    ADD COLUMN IF NOT EXISTS payment_link_token TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS idx_ar_payment_link_token
    ON ar_open_items (payment_link_token) WHERE payment_link_token IS NOT NULL;

ALTER TABLE efris_submissions
    ADD COLUMN IF NOT EXISTS adapter_mode TEXT,
    ADD COLUMN IF NOT EXISTS ura_invoice_no TEXT;
