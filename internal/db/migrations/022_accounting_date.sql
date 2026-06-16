-- 022: Accounting date for fiscal-period control
--
-- Period close keyed off wall-clock posting time (time.Now()) instead of the
-- entry's accounting date, so closing a month did not protect entries dated to
-- that month but posted later, and reports could not be period-bounded. Add an
-- explicit accounting_date and backfill it from the best available timestamp.
ALTER TABLE journal_entries
    ADD COLUMN IF NOT EXISTS accounting_date DATE NOT NULL DEFAULT CURRENT_DATE;

UPDATE journal_entries
SET accounting_date = COALESCE(posted_at, created_at)::date
WHERE accounting_date = CURRENT_DATE AND COALESCE(posted_at, created_at) IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_journal_accounting_date ON journal_entries(accounting_date);
