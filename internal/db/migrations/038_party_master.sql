-- 038: Customer / vendor party master. AR/AP open items reference parties by free
-- string today (customer_ref / vendor_ref); these tables give the frontend a real
-- list to populate supplier/customer dropdowns and a place to create new ones
-- inline. They are deliberately lightweight — the canonical org-wide party master
-- still lives in CRM; this is finance's own billing-party list keyed by code.
CREATE TABLE IF NOT EXISTS customers (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    entity_id  UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000001' REFERENCES entities(id),
    code       TEXT NOT NULL,
    name       TEXT NOT NULL,
    email      TEXT,
    phone      TEXT,
    currency   TEXT NOT NULL DEFAULT 'UGX',
    active     BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (entity_id, code)
);

CREATE TABLE IF NOT EXISTS vendors (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    entity_id  UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000001' REFERENCES entities(id),
    code       TEXT NOT NULL,
    name       TEXT NOT NULL,
    email      TEXT,
    phone      TEXT,
    currency   TEXT NOT NULL DEFAULT 'UGX',
    active     BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (entity_id, code)
);
