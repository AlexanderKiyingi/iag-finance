-- Safe monotonic journal entry numbers under concurrent writers

CREATE SEQUENCE IF NOT EXISTS journal_entry_number_seq START 1;
