-- 070: External reference map
--
-- Records which foreign record a finance row came from. Warehouse has carried
-- this table since its receipts work (wh_external_refs); finance had no
-- equivalent, so anything loading records from another system had to invent its
-- own correlation scheme or re-derive target ids on every pass.
--
-- The UNIQUE key is what makes a load idempotent: re-running an import upserts
-- against (source_service, source_type, source_id) instead of duplicating rows.
--
-- Two columns beyond the warehouse shape support systems that keep running
-- alongside this one rather than being imported once:
--
--   origin          which side last wrote the row. A relay that writes records
--                   from another system stamps its own label here, and the
--                   relay running the other direction skips those rows. Without
--                   it two relays echo each other's writes forever.
--   source_version  the source's own monotonic cursor for that record. Lets a
--                   replayed or out-of-order change be recognised as stale and
--                   dropped rather than overwriting newer data.

CREATE TABLE IF NOT EXISTS external_refs (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    source_service TEXT NOT NULL,
    source_type    TEXT NOT NULL,
    source_id      TEXT NOT NULL,
    target_type    TEXT NOT NULL,
    target_id      UUID NOT NULL,
    origin         TEXT NOT NULL DEFAULT 'platform',
    source_version BIGINT NOT NULL DEFAULT 0,
    synced_at      TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (source_service, source_type, source_id)
);

-- Reverse lookup: given a finance row, what did it come from.
CREATE INDEX IF NOT EXISTS idx_external_refs_target
    ON external_refs (target_type, target_id);

-- Relay progress: highest cursor seen per source, and the unsynced backlog.
CREATE INDEX IF NOT EXISTS idx_external_refs_cursor
    ON external_refs (source_service, source_version DESC);
