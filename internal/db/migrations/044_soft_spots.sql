-- 044: Control soft-spots
--
--   1. restrict_to_natural_side on chart_of_accounts (off by default): when an
--      operator opts an account in, a posting to its non-natural side is rejected
--      in the ledger service. Debiting a liability is legitimate accounting in
--      general (e.g. paying down AP), so this is opt-in per account, not global.
--   2. A trigger scoping journal-line dimensions to the entry's entity: a cost
--      centre or project on a line must belong to the same entity as its journal
--      entry, closing the gap where dimensions were unenforced across entities.

ALTER TABLE chart_of_accounts
    ADD COLUMN IF NOT EXISTS restrict_to_natural_side BOOLEAN NOT NULL DEFAULT FALSE;

CREATE OR REPLACE FUNCTION assert_dimension_entity() RETURNS trigger AS $$
DECLARE
    entry_entity UUID;
    dim_entity   UUID;
BEGIN
    IF NEW.cost_center_id IS NULL AND NEW.project_id IS NULL THEN
        RETURN NEW;
    END IF;
    SELECT entity_id INTO entry_entity FROM journal_entries WHERE id = NEW.journal_entry_id;
    IF NEW.cost_center_id IS NOT NULL THEN
        SELECT entity_id INTO dim_entity FROM cost_centers WHERE id = NEW.cost_center_id;
        IF dim_entity IS DISTINCT FROM entry_entity THEN
            RAISE EXCEPTION 'cost center % does not belong to the entry entity %', NEW.cost_center_id, entry_entity
                USING ERRCODE = 'check_violation';
        END IF;
    END IF;
    IF NEW.project_id IS NOT NULL THEN
        SELECT entity_id INTO dim_entity FROM projects WHERE id = NEW.project_id;
        IF dim_entity IS DISTINCT FROM entry_entity THEN
            RAISE EXCEPTION 'project % does not belong to the entry entity %', NEW.project_id, entry_entity
                USING ERRCODE = 'check_violation';
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_journal_lines_dimension_entity ON journal_lines;
CREATE TRIGGER trg_journal_lines_dimension_entity
    BEFORE INSERT ON journal_lines
    FOR EACH ROW EXECUTE FUNCTION assert_dimension_entity();
