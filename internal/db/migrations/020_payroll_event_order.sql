-- 020: Payroll employee mirror — event ordering guard
--
-- The mirror was unconditional last-writer-wins on employee_no, so a redelivered
-- or out-of-order 'erp.employee.updated' could overwrite a later
-- 'erp.employee.terminated' and resurrect a terminated employee. Track the
-- source event time so the upsert can ignore stale events.
ALTER TABLE payroll_employee_refs
    ADD COLUMN IF NOT EXISTS last_event_at TIMESTAMPTZ NOT NULL DEFAULT '1970-01-01T00:00:00Z';
