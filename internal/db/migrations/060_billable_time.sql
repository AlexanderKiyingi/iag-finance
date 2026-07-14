-- 060: billable-time (unbilled) subledger. Records billable time entries so they
-- persist and can later be pulled into an invoice. No GL is posted at capture —
-- revenue is recognised when the time is invoiced (avoids premature recognition).
CREATE TABLE IF NOT EXISTS billable_time_entries (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    entry_ref   TEXT NOT NULL UNIQUE,
    customer    TEXT NOT NULL DEFAULT '',
    employee    TEXT NOT NULL DEFAULT '',
    project     TEXT NOT NULL DEFAULT '',
    hours       NUMERIC(12, 2) NOT NULL DEFAULT 0,
    rate        NUMERIC(18, 2) NOT NULL DEFAULT 0,
    amount      NUMERIC(18, 2) NOT NULL DEFAULT 0,
    work_date   DATE NOT NULL,
    currency    TEXT NOT NULL DEFAULT 'UGX',
    notes       TEXT NOT NULL DEFAULT '',
    status      TEXT NOT NULL DEFAULT 'unbilled',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
