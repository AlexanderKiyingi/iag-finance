-- Payroll boundary: mirror ERP HR events for journal prep (no payroll run engine here).

CREATE TABLE IF NOT EXISTS payroll_employee_refs (
    employee_no      TEXT PRIMARY KEY,
    user_id          UUID,
    first_name       TEXT NOT NULL DEFAULT '',
    last_name        TEXT NOT NULL DEFAULT '',
    department_code  TEXT,
    job_title        TEXT NOT NULL DEFAULT '',
    employment_type  TEXT NOT NULL DEFAULT '',
    status           TEXT NOT NULL DEFAULT 'active',
    operator_ref     TEXT,
    last_event_id    TEXT NOT NULL,
    last_event_type  TEXT NOT NULL,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS payroll_employee_refs_status_idx ON payroll_employee_refs (status);

CREATE TABLE IF NOT EXISTS payroll_leave_accruals (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    leave_request_id   TEXT NOT NULL,
    employee_no        TEXT NOT NULL,
    leave_type_code    TEXT NOT NULL,
    starts_on          DATE NOT NULL,
    ends_on            DATE NOT NULL,
    days               NUMERIC(6, 2) NOT NULL DEFAULT 0,
    accrual_status     TEXT NOT NULL,
    source_event_id    TEXT NOT NULL UNIQUE,
    source_event_type  TEXT NOT NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS payroll_leave_accruals_employee_idx
    ON payroll_leave_accruals (employee_no, created_at DESC);

CREATE INDEX IF NOT EXISTS payroll_leave_accruals_status_idx
    ON payroll_leave_accruals (accrual_status);
