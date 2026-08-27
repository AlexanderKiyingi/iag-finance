-- 069: Widen general-ledger amounts to four decimal places
--
-- journal_lines stored debit/credit as NUMERIC(18, 2) while source systems that
-- post into the ledger carry four. Rounding on the way in is not a cosmetic
-- loss here: assert_journal_balanced (018) compares SUM(debit) to SUM(credit)
-- exactly, so an entry that balances at four decimal places but not at two is
-- rejected outright rather than quietly rounded. Widening the columns removes
-- that rounding site and lets four-decimal history post without loss.
--
-- Widening is strictly permissive — every existing two-decimal value is
-- representable at four and keeps its exact value, and nothing that writes
-- two-decimal amounts needs to change.
--
-- OPERATOR NOTE: changing a numeric column's SCALE rewrites the table and holds
-- ACCESS EXCLUSIVE for the duration. On a large journal_lines run this in a
-- maintenance window, not alongside posting traffic.

-- A table rewrite takes ACCESS EXCLUSIVE. Without a timeout, an ALTER that
-- cannot get the lock immediately waits in the lock queue — and every read that
-- arrives behind it queues too, so a migration run against live traffic stalls
-- the service rather than just itself. Failing after ten seconds leaves the
-- transaction rolled back and the table untouched; re-run it in a quiet window.
SET LOCAL lock_timeout = '10s';

ALTER TABLE journal_lines
    ALTER COLUMN debit  TYPE NUMERIC(18, 4),
    ALTER COLUMN credit TYPE NUMERIC(18, 4);

-- The balance assertion declared its accumulators as NUMERIC(18, 2), which put
-- the same rounding back inside the trigger: two sums differing only in the
-- third or fourth decimal compared equal, so widening the columns alone would
-- have let a genuinely unbalanced entry post. Recreate the function with the
-- accumulators at the column precision.
--
-- Body is otherwise identical to 018. The constraint triggers already point at
-- this function by name and are deliberately not redefined here.
CREATE OR REPLACE FUNCTION assert_journal_balanced() RETURNS trigger AS $$
DECLARE
    eid UUID;
    total_debit NUMERIC(18, 4);
    total_credit NUMERIC(18, 4);
    entry_status TEXT;
BEGIN
    IF TG_TABLE_NAME = 'journal_entries' THEN
        eid := COALESCE(NEW.id, OLD.id);
    ELSE
        eid := COALESCE(NEW.journal_entry_id, OLD.journal_entry_id);
    END IF;

    SELECT status INTO entry_status FROM journal_entries WHERE id = eid;
    -- Entry deleted (cascade) or still a draft: nothing to assert.
    IF entry_status IS NULL OR entry_status <> 'posted' THEN
        RETURN NULL;
    END IF;

    SELECT COALESCE(SUM(debit), 0), COALESCE(SUM(credit), 0)
      INTO total_debit, total_credit
      FROM journal_lines WHERE journal_entry_id = eid;

    IF total_debit <> total_credit THEN
        RAISE EXCEPTION
            'journal entry % is not balanced: debit % <> credit %',
            eid, total_debit, total_credit
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;
