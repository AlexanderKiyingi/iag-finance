-- 021: Journal reversal linkage
--
-- A posted entry is immutable; the correct way to undo it is a reversing entry
-- (a mirror-image posting), not an edit. This column links a reversal entry to
-- the entry it reverses, and the original is moved to status 'reversed'.
ALTER TABLE journal_entries
    ADD COLUMN IF NOT EXISTS reverses_entry_id UUID REFERENCES journal_entries(id);

CREATE INDEX IF NOT EXISTS idx_journal_reverses ON journal_entries(reverses_entry_id)
    WHERE reverses_entry_id IS NOT NULL;
