-- Payroll → general-ledger bridge.
--
-- Finance already mirrors ERP employee/leave data (015_payroll_erp_mirror) but
-- never posted payroll to the GL: a finalized payroll run produced no journal
-- entry, so salary expense and statutory payables never hit the books. This
-- migration adds the payroll posting accounts and a payroll_runs ledger that
-- records each run and the journal entry it booked.

-- Posting accounts (idempotent — safe alongside the programmatic chart seed).
INSERT INTO chart_of_accounts (code, name, account_type) VALUES
    ('5200', 'Salary & Wages Expense', 'expense'),
    ('2200', 'PAYE Payable',           'liability'),
    ('2210', 'NSSF Payable',           'liability'),
    ('2220', 'Net Salaries Payable',   'liability'),
    ('2230', 'Other Payroll Deductions Payable', 'liability')
ON CONFLICT (code) DO NOTHING;

-- One row per finalized payroll run. run_ref is the ERP/operator-supplied
-- idempotency key; a run is posted exactly once.
CREATE TABLE IF NOT EXISTS payroll_runs (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_ref          TEXT NOT NULL UNIQUE,
    period           TEXT NOT NULL,                 -- 'YYYY-MM'
    gross            NUMERIC(20, 2) NOT NULL,
    paye             NUMERIC(20, 2) NOT NULL DEFAULT 0,
    nssf             NUMERIC(20, 2) NOT NULL DEFAULT 0,
    other_deductions NUMERIC(20, 2) NOT NULL DEFAULT 0,
    net              NUMERIC(20, 2) NOT NULL,
    currency         TEXT NOT NULL DEFAULT 'UGX',
    status           TEXT NOT NULL DEFAULT 'posted',
    journal_entry_id UUID REFERENCES journal_entries(id) ON DELETE SET NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
