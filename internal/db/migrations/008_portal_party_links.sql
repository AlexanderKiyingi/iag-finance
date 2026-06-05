-- Phase 4: portal user → party linkage for vendor AP portal reads

CREATE TABLE IF NOT EXISTS portal_party_links (
  platform_user_id UUID PRIMARY KEY,
  party_id UUID NOT NULL,
  party_business_id TEXT NOT NULL DEFAULT '',
  supplier_type TEXT NOT NULL DEFAULT '',
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_portal_party_links_party
  ON portal_party_links (party_id);
