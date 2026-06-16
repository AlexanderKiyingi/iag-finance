-- 018: General-ledger integrity guards
--
-- Two database-level invariants that were previously only enforced in Go:
--   1. A source event books at most one journal entry (idempotency backstop for
--      the consumer / payments / adjustments).
--   2. A posted journal entry must balance: SUM(debit) = SUM(credit).

-- 1. Idempotency: replace the non-unique source_event index with a partial
--    UNIQUE index. Concurrent redelivery of the same event now collides here
--    instead of silently double-booking the ledger.
DROP INDEX IF EXISTS idx_journal_source_event;
CREATE UNIQUE INDEX IF NOT EXISTS uq_journal_source_event
    ON journal_entries (source_event_id)
    WHERE source_event_id IS NOT NULL;

-- 2. Balanced-entry enforcement. The check is DEFERRABLE INITIALLY DEFERRED so a
--    multi-line entry can be assembled within a transaction and is validated
--    once, at commit. Only 'posted' entries are checked — drafts may be in flux.
CREATE OR REPLACE FUNCTION assert_journal_balanced() RETURNS trigger AS $$
DECLARE
    eid UUID;
    total_debit NUMERIC(18, 2);
    total_credit NUMERIC(18, 2);
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

DROP TRIGGER IF EXISTS trg_journal_lines_balanced ON journal_lines;
CREATE CONSTRAINT TRIGGER trg_journal_lines_balanced
    AFTER INSERT OR UPDATE OR DELETE ON journal_lines
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION assert_journal_balanced();

DROP TRIGGER IF EXISTS trg_journal_entries_balanced ON journal_entries;
CREATE CONSTRAINT TRIGGER trg_journal_entries_balanced
    AFTER INSERT OR UPDATE ON journal_entries
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION assert_journal_balanced();
