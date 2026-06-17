-- 034: Recurring invoice schedules. A worker generates + issues an invoice from
-- the template each time next_run falls due, then advances next_run by cadence.
CREATE TABLE IF NOT EXISTS recurring_invoices (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    entity_id    UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000001' REFERENCES entities(id),
    customer_ref TEXT NOT NULL,
    currency     TEXT NOT NULL DEFAULT 'UGX',
    cadence      TEXT NOT NULL CHECK (cadence IN ('weekly', 'monthly')),
    next_run     DATE NOT NULL,
    template     JSONB NOT NULL DEFAULT '[]',  -- [{description, quantity, unitPrice, taxCode}]
    notes        TEXT NOT NULL DEFAULT '',
    active       BOOLEAN NOT NULL DEFAULT TRUE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_recurring_due ON recurring_invoices(next_run) WHERE active;
