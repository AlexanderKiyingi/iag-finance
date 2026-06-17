-- 033: Invoice model (line items + tax) layered over AR open items.
-- A draft invoice is built with lines; issuing it creates the AR open item and
-- books the GL (Dr AR / Cr Revenue / Cr Output VAT). The existing AR PDF and
-- payment-link continue to work off the resulting open item.
CREATE TABLE IF NOT EXISTS invoices (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    entity_id    UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000001' REFERENCES entities(id),
    number       TEXT NOT NULL,
    customer_ref TEXT NOT NULL,
    currency     TEXT NOT NULL DEFAULT 'UGX',
    issue_date   DATE,
    due_date     DATE,
    status       TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'issued', 'paid', 'void')),
    subtotal     NUMERIC(18, 2) NOT NULL DEFAULT 0,
    tax_total    NUMERIC(18, 2) NOT NULL DEFAULT 0,
    total        NUMERIC(18, 2) NOT NULL DEFAULT 0,
    notes        TEXT NOT NULL DEFAULT '',
    document_ref TEXT,  -- ar_open_items.document_ref once issued
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (entity_id, number)
);

CREATE TABLE IF NOT EXISTS invoice_lines (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    invoice_id  UUID NOT NULL REFERENCES invoices(id) ON DELETE CASCADE,
    description TEXT NOT NULL DEFAULT '',
    quantity    NUMERIC(18, 4) NOT NULL DEFAULT 1,
    unit_price  NUMERIC(18, 2) NOT NULL DEFAULT 0,
    tax_code    TEXT,
    line_total  NUMERIC(18, 2) NOT NULL DEFAULT 0,
    tax_amount  NUMERIC(18, 2) NOT NULL DEFAULT 0,
    line_order  INT NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_invoice_lines_invoice ON invoice_lines(invoice_id);

CREATE SEQUENCE IF NOT EXISTS invoice_number_seq START 1000;
