-- 062: Shared party_id on the finance party master for the cross-service vendor
-- mesh (SCM ⇄ procurement ⇄ finance). Vendors/customers are keyed locally by
-- (entity_id, code); party_id is the canonical UUID correlated across services so
-- a vendor created in procurement or SCM upserts to the same finance row. Nullable
-- (legacy rows keep NULL until edited/synced); the partial unique index keeps it
-- 1:1 without forcing identity on pre-mesh rows.
ALTER TABLE vendors   ADD COLUMN IF NOT EXISTS party_id UUID;
ALTER TABLE customers ADD COLUMN IF NOT EXISTS party_id UUID;

CREATE UNIQUE INDEX IF NOT EXISTS idx_vendors_party_id
    ON vendors (party_id) WHERE party_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_customers_party_id
    ON customers (party_id) WHERE party_id IS NOT NULL;
