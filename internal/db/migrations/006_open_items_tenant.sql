-- Align AR/AP open items with tenant-scoped inbox tables (005_operational, operational seed).

ALTER TABLE ar_open_items
    ADD COLUMN IF NOT EXISTS tenant_id TEXT NOT NULL DEFAULT 'default';

ALTER TABLE ap_open_items
    ADD COLUMN IF NOT EXISTS tenant_id TEXT NOT NULL DEFAULT 'default';

CREATE INDEX IF NOT EXISTS idx_ar_tenant ON ar_open_items (tenant_id);
CREATE INDEX IF NOT EXISTS idx_ap_tenant ON ap_open_items (tenant_id);
