-- Employer social-security contribution.
--
-- A payroll run booked the employee's side only: gross salary expense against
-- PAYE, NSSF withheld, other deductions and net pay. The employer's own NSSF
-- contribution — 10% of pensionable pay in Uganda, twice what the employee
-- pays — appeared nowhere. It is not a deduction from anyone's salary; it is a
-- separate cost the employer owes the fund, and leaving it out understates both
-- staff costs and the liability to NSSF every month payroll runs.
--
-- The employee's 5% keeps posting to the same NSSF payable account. Both halves
-- are owed to the same fund and remitted together, so they belong on the same
-- payable; only the debit side differs.

INSERT INTO chart_of_accounts (code, name, account_type) VALUES
    ('5210', 'Employer NSSF Contribution', 'expense')
ON CONFLICT (code) DO NOTHING;

-- Recorded on the run so the figure can be reported without re-deriving it from
-- the journal. Nullable and defaulted: runs posted before this migration had no
-- employer figure, and zero would assert one was computed and came to nothing.
ALTER TABLE payroll_runs
    ADD COLUMN IF NOT EXISTS employer_nssf NUMERIC(20, 2);
