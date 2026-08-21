-- 067: A subledger for the Payments Clearing control account (1050).
--
-- Migration 056 routed every operational disbursement through 1050 rather than
-- guessing its ultimate expense/asset/AP account, on the understanding that
-- finance would "reconcile this clearing account against the originating
-- document". Nothing was built to do that with. The account had a general-ledger
-- balance and no subledger, so there was no list of what was sitting in it, no
-- ageing, and no way to tell a correctly-pending settlement from one stranded
-- there because its originating document never turned up.
--
-- 2150 GR/IR already has its subledger (grni_accruals, migration 025) and so
-- reconciles. This gives 1050 the same treatment: one row per settled
-- disbursement, cleared against the document it belongs to, and a difference
-- against the GL that should be zero.

CREATE TABLE IF NOT EXISTS payments_clearing_items (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    entity_id      UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000001' REFERENCES entities(id),
    -- The payments instruction this settlement came from. Unique, so a
    -- redelivered payments.settled updates rather than duplicating — the same
    -- guarantee the journal gets from source_event_id.
    instruction_id TEXT NOT NULL,
    reference_number TEXT NOT NULL DEFAULT '',
    -- Which service asked for the money and what kind of disbursement it was;
    -- together they say which document should eventually clear this row.
    origin_service TEXT NOT NULL DEFAULT '',
    category       TEXT NOT NULL DEFAULT '',
    party_business_id TEXT NOT NULL DEFAULT '',
    provider_ref   TEXT NOT NULL DEFAULT '',
    amount         NUMERIC(18, 2) NOT NULL CHECK (amount > 0),
    currency       TEXT NOT NULL DEFAULT 'UGX',
    settled_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    status         TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'cleared')),
    -- The document this disbursement was for (vendor invoice, payroll run, loan
    -- agreement, claim). Set when the row is cleared.
    cleared_against TEXT,
    cleared_at     TIMESTAMPTZ,
    cleared_by     UUID,
    -- The journal entry that reclassified 1050 out to its final account, when
    -- clearing posted one.
    journal_entry_id UUID REFERENCES journal_entries(id) ON DELETE SET NULL,

    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- A cleared row must say what cleared it. Without this the status could be
    -- flipped with no record of which document absorbed the money, which is the
    -- one fact the reconciliation exists to establish.
    CONSTRAINT payments_clearing_cleared_has_document CHECK (
        status <> 'cleared' OR (cleared_against IS NOT NULL AND cleared_against <> '')
    )
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_payments_clearing_instruction
    ON payments_clearing_items (instruction_id);
CREATE INDEX IF NOT EXISTS idx_payments_clearing_open
    ON payments_clearing_items (entity_id, settled_at) WHERE status = 'open';
CREATE INDEX IF NOT EXISTS idx_payments_clearing_category
    ON payments_clearing_items (category);
