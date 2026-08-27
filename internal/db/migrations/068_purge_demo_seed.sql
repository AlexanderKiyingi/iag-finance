-- Purge the finance demo dataset applied by RunDemoSeed and RunOperationalSeed
-- (internal/db/seed/demo.sql and internal/db/seed/operational.sql).
--
-- demo.sql wrote pre-rendered HTML rows into table_rows under four fixed table_ids;
-- operational.sql wrote the demo bank accounts, AP inbox and cherry-intake lines that
-- back the same views. Both are removed here as part of the platform-wide seed purge.
--
-- Deliberately preserved:
--   * The chart of accounts seeded by ledger.Seed / SeedChartOfAccounts — the posting
--     backbone every journal entry validates against.
--   * The reference data in earlier migrations: banks (039), COGS and inventory
--     account mappings (040, 032), the VAT engine (029), FX (028), the fixed-asset
--     register defaults (027) and the IFRS/IAS configuration (041-047, 052-067).
--
-- Only the seeds' fixed identifiers are targeted, so records posted through the
-- service since are untouched.
--
-- The runner wraps each migration in its own transaction, so this file does not open
-- one: a COMMIT here would end that transaction early.

-- ---- demo.sql: pre-rendered UI rows ----------------------------------------
DELETE FROM table_rows
WHERE table_id IN ('seed_coa', 'seed_bank_cash', 'seed_ap_inbox', 'seed_cherry_intake');

-- ---- operational.sql: structured demo rows ---------------------------------
DELETE FROM cherry_intake_lines
WHERE intake_code IN ('CI-09472', 'CI-09471', 'CI-09470');

DELETE FROM ap_open_items
WHERE document_ref IN ('INV-AP-2026-04412', 'INV-AP-2026-04411', 'INV-AP-2026-04410');

DELETE FROM bank_accounts
WHERE code IN ('1110', '1120', '1130');
