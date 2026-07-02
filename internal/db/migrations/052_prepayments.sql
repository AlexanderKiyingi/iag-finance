-- 052: IAS 1 matching — prepaid-expense amortization (expense-side mirror of the
-- IFRS 15 deferral engine in 046).
--
-- A prepayment (insurance, rent, a subscription paid up front) must be carried as
-- a prepaid asset and expensed over the period it covers, not all at once when
-- paid. This adds:
--   * Prepaid Expenses (1250, asset);
--   * prepaid_schedules + prepaid_schedule_lines: creating a schedule capitalises
--     the outlay (Dr 1250 / Cr funding account) and lays out straight-line slices;
--     a periodic amortization run releases each due slice to expense
--     (Dr <expense account> / Cr 1250).
--
-- Straight-line only (prepayments amortize evenly); the target expense account and
-- the funding account are stored per schedule. New account added to defaultAccounts.

INSERT INTO chart_of_accounts (code, name, account_type) VALUES
    ('1250', 'Prepaid Expenses', 'asset')
ON CONFLICT (code) DO NOTHING;

CREATE TABLE IF NOT EXISTS prepaid_schedules (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    source_ref          TEXT NOT NULL,                 -- bill number or contract ref
    entity_id           UUID,
    total               NUMERIC(18, 2) NOT NULL CHECK (total > 0),
    currency            TEXT NOT NULL DEFAULT 'UGX',
    expense_code        TEXT NOT NULL,                 -- account amortized into
    funding_code        TEXT NOT NULL,                 -- account credited at capitalization
    start_period        TEXT NOT NULL,                 -- 'YYYY-MM'
    periods             INT NOT NULL DEFAULT 1 CHECK (periods >= 1),
    status              TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'completed')),
    capitalize_entry_id UUID REFERENCES journal_entries(id) ON DELETE SET NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (source_ref)
);

CREATE TABLE IF NOT EXISTS prepaid_schedule_lines (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    schedule_id      UUID NOT NULL REFERENCES prepaid_schedules(id) ON DELETE CASCADE,
    period           TEXT NOT NULL,                    -- 'YYYY-MM' this slice is expensed in
    amount           NUMERIC(18, 2) NOT NULL CHECK (amount >= 0),
    journal_entry_id UUID REFERENCES journal_entries(id) ON DELETE SET NULL,
    recognized_at    TIMESTAMPTZ,
    UNIQUE (schedule_id, period)
);

CREATE INDEX IF NOT EXISTS idx_prepaid_lines_due
    ON prepaid_schedule_lines (period) WHERE journal_entry_id IS NULL;
