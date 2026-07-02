-- 039: IFRS 9 — expected-credit-loss (ECL) allowance + formal write-off / recovery
--
-- Before this, receivables were carried gross: there was no allowance for
-- doubtful debts and no write-off path (operators improvised with credit notes,
-- which wrongly reverse revenue instead of recognising a bad-debt expense).
-- This adds:
--   * a contra-asset allowance (1190) and a bad-debt expense (5400), so AR is
--     presented net of expected loss on the balance sheet;
--   * a policy table of loss rates per aging bucket (the simplified ECL matrix);
--   * a period provisioning run that books only the MOVEMENT to the target
--     allowance (idempotent per period);
--   * a write-off that consumes the allowance (Dr 1190, remainder Dr 5400 / Cr
--     1100) and a recovery that credits Bad Debt Recovery (4300).
--
-- New GL accounts are also added to defaultAccounts (repository.go) so fresh
-- installs match; ON CONFLICT keeps this safe on already-seeded databases.

INSERT INTO chart_of_accounts (code, name, account_type) VALUES
    ('1190', 'Allowance for Doubtful Debts', 'asset'),
    ('5400', 'Bad Debt Expense',            'expense'),
    ('4300', 'Bad Debt Recovery',           'revenue')
ON CONFLICT (code) DO NOTHING;

-- Simplified-approach ECL loss-rate matrix, keyed on the same aging buckets the
-- ARAging report already produces. Rates are policy defaults; an operator tunes
-- them via UPSERT. loss_rate is a fraction (0.05 = 5%).
CREATE TABLE IF NOT EXISTS ecl_rates (
    bucket     TEXT PRIMARY KEY CHECK (bucket IN ('current', '1-30', '31-60', '61-90', '90+')),
    loss_rate  NUMERIC(6, 4) NOT NULL DEFAULT 0 CHECK (loss_rate >= 0 AND loss_rate <= 1),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO ecl_rates (bucket, loss_rate) VALUES
    ('current', 0.00),
    ('1-30',    0.01),
    ('31-60',   0.05),
    ('61-90',   0.20),
    ('90+',     0.50)
ON CONFLICT (bucket) DO NOTHING;

-- One row per period a provisioning run posts. computed_amount is the target
-- allowance; movement is what was actually booked (target − prior allowance).
CREATE TABLE IF NOT EXISTS ar_provisions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    period          TEXT NOT NULL UNIQUE,          -- 'YYYY-MM'
    method          TEXT NOT NULL DEFAULT 'ecl_matrix',
    computed_amount NUMERIC(18, 2) NOT NULL DEFAULT 0,
    movement        NUMERIC(18, 2) NOT NULL DEFAULT 0,
    journal_entry_id UUID REFERENCES journal_entries(id) ON DELETE SET NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Formal write-off ledger. recovered_amount accumulates later recoveries.
CREATE TABLE IF NOT EXISTS ar_writeoffs (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    document_ref     TEXT NOT NULL UNIQUE,
    customer_ref     TEXT NOT NULL DEFAULT '',
    amount           NUMERIC(18, 2) NOT NULL CHECK (amount > 0),
    covered_by_allowance NUMERIC(18, 2) NOT NULL DEFAULT 0,
    currency         TEXT NOT NULL DEFAULT 'UGX',
    reason           TEXT NOT NULL DEFAULT '',
    recovered_amount NUMERIC(18, 2) NOT NULL DEFAULT 0,
    journal_entry_id UUID REFERENCES journal_entries(id) ON DELETE SET NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
