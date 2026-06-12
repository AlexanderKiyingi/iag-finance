-- Fiscal-period close control for the general ledger.
--
-- Posting a journal entry into a closed accounting period is a core internal
-- control. Until now the only period check lived in the advisory
-- /ledger/validate-posting simulator (a hardcoded month regex) and was never
-- enforced on POST /ledger/entries/:id/post, so a client could post straight
-- into any period.
--
-- A period is OPEN by default (absence of a row), so this migration is inert
-- until an operator explicitly closes a period. Closing 'YYYY-MM' blocks
-- further postings dated in that month; reopening lifts the block.
CREATE TABLE IF NOT EXISTS fiscal_periods (
    period     TEXT PRIMARY KEY,            -- 'YYYY-MM'
    status     TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'closed')),
    closed_at  TIMESTAMPTZ,
    closed_by  UUID,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
