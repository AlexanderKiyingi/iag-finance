-- Accrued leave: the obligation for leave earned and not yet taken.
--
-- Approved leave already arrives from HR and lands in payroll_leave_accruals,
-- where it is readable and never booked. Untaken leave is money owed to staff,
-- so the balance sheet understates the obligation and the expense falls in the
-- period leave is taken rather than earned.
--
-- Two of the three inputs a computed liability needs are missing from the
-- platform: HR reports leave *taken* but never the balance *earned*, and no
-- service holds a pay rate to value a day with. Until both exist, the liability
-- is stated the same way payroll totals already are — by whoever calculates
-- payroll — and booked here against provision_ref as the idempotency key.
--
-- The expense side is 5200 Salary & Wages Expense, the account payroll already
-- uses. A separate leave-expense account would split staff cost across two
-- lines for no reporting benefit.

INSERT INTO chart_of_accounts (code, name, account_type)
VALUES ('2240', 'Accrued Leave', 'liability')
ON CONFLICT (code) DO NOTHING;

-- One row per stated provision movement. Amount is the *change* in the
-- liability for the period, not the closing balance: booking a balance would
-- double-count every period it did not move.
CREATE TABLE IF NOT EXISTS payroll_leave_provisions (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provision_ref    TEXT NOT NULL UNIQUE,
    period           TEXT NOT NULL,                 -- 'YYYY-MM'
    amount           NUMERIC(20, 2) NOT NULL,       -- signed: + increases the liability
    currency         TEXT NOT NULL DEFAULT 'UGX',
    note             TEXT NOT NULL DEFAULT '',
    journal_entry_id UUID REFERENCES journal_entries (id) ON DELETE SET NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS payroll_leave_provisions_period_idx
    ON payroll_leave_provisions (period);
