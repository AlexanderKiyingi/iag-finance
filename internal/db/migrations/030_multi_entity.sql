-- 030: Multi-entity + GL dimensions.
--
-- Reintroduce an entity dimension so the platform can keep books for more than
-- one legal/accounting entity and consolidate them. The chart of accounts stays
-- SHARED; the entity lives on transactions (standard ERP pattern). All existing
-- data belongs to a seeded DEFAULT entity (fixed id), so this is a pure backfill —
-- single-entity behaviour is unchanged. Also add nullable cost-centre / project
-- dimension columns on journal lines for Phase 6 (projects / job costing).

CREATE TABLE IF NOT EXISTS entities (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code          TEXT NOT NULL UNIQUE,
    name          TEXT NOT NULL,
    base_currency TEXT NOT NULL DEFAULT 'UGX',
    parent_id     UUID REFERENCES entities(id),
    active        BOOLEAN NOT NULL DEFAULT TRUE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Fixed default entity id — referenced as the column default below.
INSERT INTO entities (id, code, name)
VALUES ('00000000-0000-0000-0000-000000000001', 'DEFAULT', 'Default Entity')
ON CONFLICT (id) DO NOTHING;

-- entity_id on transaction tables, defaulting to the DEFAULT entity so existing
-- rows backfill automatically and inserts that don't specify one still work.
ALTER TABLE journal_entries
    ADD COLUMN IF NOT EXISTS entity_id UUID NOT NULL
    DEFAULT '00000000-0000-0000-0000-000000000001' REFERENCES entities(id);
ALTER TABLE ar_open_items
    ADD COLUMN IF NOT EXISTS entity_id UUID NOT NULL
    DEFAULT '00000000-0000-0000-0000-000000000001' REFERENCES entities(id);
ALTER TABLE ap_open_items
    ADD COLUMN IF NOT EXISTS entity_id UUID NOT NULL
    DEFAULT '00000000-0000-0000-0000-000000000001' REFERENCES entities(id);

CREATE INDEX IF NOT EXISTS idx_journal_entries_entity ON journal_entries(entity_id);
CREATE INDEX IF NOT EXISTS idx_ar_entity ON ar_open_items(entity_id);
CREATE INDEX IF NOT EXISTS idx_ap_entity ON ap_open_items(entity_id);

-- Optional GL dimensions (masters + reporting land in Phase 6).
ALTER TABLE journal_lines
    ADD COLUMN IF NOT EXISTS cost_center_id UUID,
    ADD COLUMN IF NOT EXISTS project_id UUID;
