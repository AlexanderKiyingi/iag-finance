-- 040: IFRS 15 — deferred & accrued revenue, scheduled recognition
--
-- Previously revenue was recognised entirely at invoice issue (point in time).
-- IFRS 15 requires revenue to track the satisfaction of performance obligations,
-- which for subscriptions/services means spreading it over time (or over
-- milestones), and recognising revenue earned-but-not-yet-billed as a contract
-- asset. This adds:
--   * Deferred Revenue (2300, contract liability) and Accrued Revenue (1200,
--     contract asset);
--   * revenue_schedules + revenue_schedule_lines: a schedule reclassifies
--     already-recognised revenue into deferred (Dr 4000 / Cr 2300) and a periodic
--     recognition run releases each due slice back (Dr 2300 / Cr 4000);
--   * performance_obligations: milestone-based recognition.
--
-- This composes with the existing IssueInvoice path (which still recognises at
-- issue); attaching a schedule spreads that revenue instead of leaving it all in
-- the issue period. New accounts are also added to defaultAccounts.

INSERT INTO chart_of_accounts (code, name, account_type) VALUES
    ('2300', 'Deferred Revenue', 'liability'),
    ('1200', 'Accrued Revenue',  'asset')
ON CONFLICT (code) DO NOTHING;

CREATE TABLE IF NOT EXISTS revenue_schedules (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    source_ref   TEXT NOT NULL,                 -- invoice number or contract ref
    entity_id    UUID,
    total        NUMERIC(18, 2) NOT NULL CHECK (total > 0),
    currency     TEXT NOT NULL DEFAULT 'UGX',
    method       TEXT NOT NULL DEFAULT 'ratable' CHECK (method IN ('ratable', 'milestone')),
    start_period TEXT NOT NULL,                 -- 'YYYY-MM'
    periods      INT NOT NULL DEFAULT 1 CHECK (periods >= 1),
    status       TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'completed')),
    defer_entry_id UUID REFERENCES journal_entries(id) ON DELETE SET NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (source_ref)
);

CREATE TABLE IF NOT EXISTS revenue_schedule_lines (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    schedule_id      UUID NOT NULL REFERENCES revenue_schedules(id) ON DELETE CASCADE,
    period           TEXT NOT NULL,             -- 'YYYY-MM' this slice is recognised in
    amount           NUMERIC(18, 2) NOT NULL CHECK (amount >= 0),
    journal_entry_id UUID REFERENCES journal_entries(id) ON DELETE SET NULL,
    recognized_at    TIMESTAMPTZ,
    UNIQUE (schedule_id, period)
);

CREATE INDEX IF NOT EXISTS idx_revsched_lines_due
    ON revenue_schedule_lines (period) WHERE journal_entry_id IS NULL;

CREATE TABLE IF NOT EXISTS performance_obligations (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    schedule_id      UUID NOT NULL REFERENCES revenue_schedules(id) ON DELETE CASCADE,
    description      TEXT NOT NULL DEFAULT '',
    amount           NUMERIC(18, 2) NOT NULL CHECK (amount > 0),
    satisfied_at     TIMESTAMPTZ,
    journal_entry_id UUID REFERENCES journal_entries(id) ON DELETE SET NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
