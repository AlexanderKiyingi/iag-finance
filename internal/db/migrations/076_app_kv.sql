-- 076: App-shell key/value store
--
-- The finance web app calls a family of endpoints that had no server anywhere
-- on the platform — /api/settings, /api/kv, /api/drafts, /api/push and
-- /api/request-email-contacts. They existed only as rewrites to the legacy Go
-- API, so on a platform-only deployment app settings could not be saved, form
-- drafts were lost on reload, and web-push could not be subscribed to at all.
--
-- All five are the same shape: a scoped JSON document under a namespace and a
-- key. Five bespoke tables would be five times the surface for one behaviour,
-- so they share this one.
--
-- scope is 'global' or 'user'.
--   global — one row per (namespace, key) for the whole tenant; admin-gated.
--   user   — one row per viewer; owner_id is the platform user id.
--
-- owner_id is NOT NULL with '' for global rows rather than nullable, because a
-- NULL would make the primary key stop enforcing uniqueness on global rows
-- (NULL never equals NULL in a unique index).
CREATE TABLE IF NOT EXISTS app_kv (
    scope      TEXT        NOT NULL CHECK (scope IN ('global', 'user')),
    owner_id   TEXT        NOT NULL DEFAULT '',
    namespace  TEXT        NOT NULL,
    key        TEXT        NOT NULL,
    value      JSONB       NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by TEXT        NOT NULL DEFAULT '',
    PRIMARY KEY (scope, owner_id, namespace, key),
    -- A user-scoped row without an owner would be readable by everyone; a
    -- global row with one would be invisible to the admins who set it.
    CONSTRAINT app_kv_owner_matches_scope CHECK (
        (scope = 'user'   AND owner_id <> '') OR
        (scope = 'global' AND owner_id  = '')
    )
);

-- Listing a namespace is the common read (all of a user's drafts, every push
-- subscription); the primary key cannot serve it because key is the last column.
CREATE INDEX IF NOT EXISTS idx_app_kv_namespace
    ON app_kv (scope, owner_id, namespace);
