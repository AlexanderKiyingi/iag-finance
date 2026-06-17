-- 028: FX gain/loss — base-currency balance invariant + accounts.
--
-- The balanced-entry trigger (migration 018) asserted NOMINAL debit = credit.
-- That holds only for single-currency entries. A realised-FX or revaluation entry
-- mixes currencies (e.g. a USD cash leg + a UGX gain line) and balances in BASE
-- currency, not nominal. Switch the invariant to SUM(debit_base) = SUM(credit_base)
-- — the true general-ledger invariant. It is equivalent for single-currency
-- entries (base = nominal × one rate), so existing behaviour is unchanged.
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

-- FX gain/loss accounts.
INSERT INTO chart_of_accounts (code, name, account_type)
VALUES
    ('7200', 'Realized FX Gain', 'revenue'),
    ('7210', 'Realized FX Loss', 'expense'),
    ('7220', 'Unrealized FX Gain/Loss', 'expense'),
    ('2900', 'FX Revaluation', 'liability')
ON CONFLICT (code) DO NOTHING;
