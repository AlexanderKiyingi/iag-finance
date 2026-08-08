-- Inputs for a computed leave liability.
--
-- Migration 064 added the liability account and an endpoint for stating the
-- figure, because the platform could not derive it: HR reported leave taken but
-- never the balance earned, and no service held a pay rate.
--
-- HR now publishes both — erp.leave.balance_changed carries days owed,
-- erp.employee.rate_changed carries what a day is worth. These tables hold what
-- arrives so the liability can be measured rather than asserted.
--
-- Only the derived daily rate crosses the boundary. Gross pay stays in HR.

CREATE TABLE IF NOT EXISTS payroll_leave_balances (
    employee_no     TEXT NOT NULL,
    leave_type_code TEXT NOT NULL,
    accrual_year    INTEGER NOT NULL,
    balance_days    NUMERIC(8, 2) NOT NULL DEFAULT 0,
    source_event_id TEXT NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (employee_no, leave_type_code, accrual_year)
);

CREATE TABLE IF NOT EXISTS payroll_employee_rates (
    employee_no    TEXT PRIMARY KEY,
    daily_rate     NUMERIC(20, 2) NOT NULL,
    currency       TEXT NOT NULL DEFAULT 'UGX',
    effective_from DATE NOT NULL,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- What the liability was last measured at, so a valuation books the movement
-- since the previous one rather than the whole obligation again. Booking a
-- balance instead of a movement is the mistake this table exists to prevent.
CREATE TABLE IF NOT EXISTS payroll_leave_valuations (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    valued_at        DATE NOT NULL,
    total_liability  NUMERIC(20, 2) NOT NULL,
    movement         NUMERIC(20, 2) NOT NULL,
    currency         TEXT NOT NULL DEFAULT 'UGX',
    employees_valued INTEGER NOT NULL DEFAULT 0,
    -- Employees with a balance but no rate: their obligation is real and
    -- unmeasured, so the count travels with the figure rather than being
    -- silently excluded from it.
    employees_unrated INTEGER NOT NULL DEFAULT 0,
    journal_entry_id UUID REFERENCES journal_entries (id) ON DELETE SET NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS payroll_leave_valuations_date_idx
    ON payroll_leave_valuations (valued_at);
