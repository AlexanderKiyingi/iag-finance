-- 055: Consolidation eliminations (IFRS 10).
--
-- Consolidated reports already aggregate across the entity tree (EntityScope);
-- this adds the elimination of intra-group activity so the group isn't double-
-- counted:
--   * journal_entries.counterparty_entity_id — tags an entry as intercompany
--     (the other entity in the transaction). When BOTH the entry's entity and its
--     counterparty are inside the consolidation scope, the whole entry is intra-
--     group and its lines are eliminated (nets IC receivables/payables AND IC
--     revenue/COGS in one rule).
--   * entities.ownership_pct — the parent's ownership fraction of THIS entity
--     (1.0 = wholly owned). Drives the investment-in-subsidiary vs subsidiary-
--     equity elimination and the non-controlling interest (NCI) share.
--   * Investment in Subsidiary (1800), Goodwill (1900), Non-Controlling Interest
--     (3200) accounts for the structural elimination.
--
-- Eliminations are computed at report time (no posted elimination entries), so a
-- consolidated statement is always in sync. New accounts are added to
-- defaultAccounts.

ALTER TABLE journal_entries
    ADD COLUMN IF NOT EXISTS counterparty_entity_id UUID REFERENCES entities(id);

CREATE INDEX IF NOT EXISTS idx_journal_entries_counterparty
    ON journal_entries(counterparty_entity_id) WHERE counterparty_entity_id IS NOT NULL;

ALTER TABLE entities
    ADD COLUMN IF NOT EXISTS ownership_pct NUMERIC(6, 4) NOT NULL DEFAULT 1.0
    CHECK (ownership_pct > 0 AND ownership_pct <= 1);

INSERT INTO chart_of_accounts (code, name, account_type) VALUES
    ('1800', 'Investment in Subsidiary',   'asset'),
    ('1900', 'Goodwill',                   'asset'),
    ('3200', 'Non-Controlling Interest',   'equity')
ON CONFLICT (code) DO NOTHING;
