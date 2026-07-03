-- 048: Revenue-recognition spec on invoices (wires IFRS 15 into the issue path)
--
-- An invoice can now carry a recognition schedule. When present, issuing the
-- invoice recognises revenue at issue as usual and then immediately defers and
-- spreads it (Dr 4000 / Cr 2300, then a monthly Dr 2300 / Cr 4000 recognition
-- run) via the migration-046 revenue_schedules — so subscription/service revenue
-- is recognised over the period it is earned rather than all at issue.
--
-- Empty method (the default) leaves the point-in-time behaviour unchanged.

ALTER TABLE invoices
    ADD COLUMN IF NOT EXISTS recognition_method  TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS recognition_periods INT  NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS recognition_start   TEXT NOT NULL DEFAULT '';
