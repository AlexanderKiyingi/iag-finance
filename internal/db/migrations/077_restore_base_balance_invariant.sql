-- 077: Restore the base-currency balance invariant that 069 reverted.
--
-- Migration 028 changed assert_journal_balanced to compare SUM(debit_base) to
-- SUM(credit_base): the true general-ledger invariant, and the only one a
-- mixed-currency entry can satisfy (a USD receivable settled with a UGX
-- realized-gain line balances in base, never in nominal). Migration 069 then
-- redefined the same function from the 018 text to widen its accumulators to
-- four decimal places — and in doing so put the NOMINAL comparison back.
--
-- Since 069 every realized FX gain or loss, and every FX revaluation, has been
-- rejected with "journal entry … is not balanced", which is exactly what
-- TestRealizedFXGainOnARPayment has failed with in CI since 27 August.
--
-- This is 028's function at 069's precision. The base columns are widened to
-- match so a four-decimal nominal amount does not lose precision on the base
-- leg either.
ALTER TABLE journal_lines
    ALTER COLUMN debit_base  TYPE NUMERIC(18, 4),
    ALTER COLUMN credit_base TYPE NUMERIC(18, 4);

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

    SELECT COALESCE(SUM(debit_base), 0), COALESCE(SUM(credit_base), 0)
      INTO total_debit, total_credit
      FROM journal_lines WHERE journal_entry_id = eid;

    IF total_debit <> total_credit THEN
        RAISE EXCEPTION
            'journal entry % is not balanced (base): debit % <> credit %',
            eid, total_debit, total_credit
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;
