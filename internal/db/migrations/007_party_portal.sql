-- Phase 4: party linkage for finance vendor portal AP lines

ALTER TABLE ap_open_items
    ADD COLUMN IF NOT EXISTS party_id UUID;

CREATE INDEX IF NOT EXISTS idx_ap_party_id ON ap_open_items (party_id);
