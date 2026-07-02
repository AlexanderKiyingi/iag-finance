-- 053: IFRS 16 leases — right-of-use asset + lease liability.
--
-- A lessee must recognise most leases on balance sheet: a right-of-use (ROU)
-- asset and a lease liability at the present value of the lease payments. Each
-- period the liability unwinds at the discount rate (interest), the payment
-- reduces it (principal), and the ROU asset is depreciated straight-line over the
-- lease term. This adds:
--   * Right-of-Use Assets (1600, asset), Accumulated Depreciation - ROU (1610,
--     contra-asset), Lease Liability (2500, liability), ROU Depreciation (5320,
--     expense), Interest Expense - Leases (5600, expense);
--   * leases + lease_schedule_lines: the amortization schedule is precomputed at
--     recognition; a periodic lease run books interest, payment and depreciation
--     for every due, un-booked line.
--
-- New accounts are also added to defaultAccounts.

INSERT INTO chart_of_accounts (code, name, account_type) VALUES
    ('1600', 'Right-of-Use Assets',                'asset'),
    ('1610', 'Accumulated Depreciation - ROU',     'asset'),
    ('2500', 'Lease Liability',                    'liability'),
    ('5320', 'Depreciation - Right-of-Use Assets', 'expense'),
    ('5600', 'Interest Expense - Leases',          'expense')
ON CONFLICT (code) DO NOTHING;

CREATE TABLE IF NOT EXISTS leases (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    lease_ref       TEXT NOT NULL,                 -- lease/contract reference
    entity_id       UUID,
    description     TEXT NOT NULL DEFAULT '',
    currency        TEXT NOT NULL DEFAULT 'UGX',
    monthly_payment NUMERIC(18, 2) NOT NULL CHECK (monthly_payment > 0),
    annual_rate     NUMERIC(9, 6) NOT NULL DEFAULT 0 CHECK (annual_rate >= 0),  -- e.g. 0.12 = 12%/yr
    term_months     INT NOT NULL CHECK (term_months >= 1),
    start_period    TEXT NOT NULL,                 -- 'YYYY-MM'
    rou_asset       NUMERIC(18, 2) NOT NULL,       -- = present value of payments
    initial_liability NUMERIC(18, 2) NOT NULL,     -- = present value of payments
    status          TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'completed')),
    recognize_entry_id UUID REFERENCES journal_entries(id) ON DELETE SET NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (lease_ref)
);

CREATE TABLE IF NOT EXISTS lease_schedule_lines (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    lease_id         UUID NOT NULL REFERENCES leases(id) ON DELETE CASCADE,
    period           TEXT NOT NULL,                -- 'YYYY-MM'
    opening_liability NUMERIC(18, 2) NOT NULL,
    interest         NUMERIC(18, 2) NOT NULL,
    payment          NUMERIC(18, 2) NOT NULL,
    principal        NUMERIC(18, 2) NOT NULL,
    closing_liability NUMERIC(18, 2) NOT NULL,
    depreciation     NUMERIC(18, 2) NOT NULL,
    journal_entry_id UUID REFERENCES journal_entries(id) ON DELETE SET NULL,
    recognized_at    TIMESTAMPTZ,
    UNIQUE (lease_id, period)
);

CREATE INDEX IF NOT EXISTS idx_lease_lines_due
    ON lease_schedule_lines (period) WHERE journal_entry_id IS NULL;
