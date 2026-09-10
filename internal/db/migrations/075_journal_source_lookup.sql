-- 075: Source-keyed journal lookup
--
-- correlation_id records which foreign document produced an entry (set on
-- create; see createJournalRequest.documentRef). Replacing a document's
-- postings means finding every entry that document produced, which is a filter
-- on (source_service, correlation_id) — a pair the existing indexes do not
-- cover: idx_journal_source_event indexes source_event_id only, which is the
-- bus event id, a different key.
--
-- Without this the replace path degrades to a sequential scan of
-- journal_entries on every document edit.

CREATE INDEX IF NOT EXISTS idx_journal_source_correlation
    ON journal_entries (source_service, correlation_id)
    WHERE correlation_id IS NOT NULL;
