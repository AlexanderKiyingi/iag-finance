-- 036: Project / cost-centre masters for job costing. Journal lines already carry
-- nullable project_id / cost_center_id (migration 030); these tables name them and
-- the project P&L report aggregates by them.
CREATE TABLE IF NOT EXISTS cost_centers (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    entity_id  UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000001' REFERENCES entities(id),
    code       TEXT NOT NULL,
    name       TEXT NOT NULL,
    active     BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (entity_id, code)
);

CREATE TABLE IF NOT EXISTS projects (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    entity_id  UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000001' REFERENCES entities(id),
    code       TEXT NOT NULL,
    name       TEXT NOT NULL,
    active     BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (entity_id, code)
);

CREATE INDEX IF NOT EXISTS idx_journal_lines_project ON journal_lines(project_id) WHERE project_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_journal_lines_cost_center ON journal_lines(cost_center_id) WHERE cost_center_id IS NOT NULL;
