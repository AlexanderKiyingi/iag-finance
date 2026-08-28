-- Customer portals and billable time from the ERP monolith
--
-- Created during the ERP monolith clone, when the tail of the migration turned
-- out to have no platform tables to land in. Recorded here so a fresh
-- environment reproduces them; IF NOT EXISTS makes this a no-op where they
-- already stand.
--
-- Cross-service references (project_id -> finance.projects) are plain columns
-- rather than foreign keys: the services deploy independently and a constraint
-- would couple their migration order. The accompanying *_name column carries
-- the source's own text, so the link survives even where the id does not.

CREATE TABLE IF NOT EXISTS finance.customer_portals (
    id                   UUID PRIMARY KEY,
    customer             TEXT NOT NULL,
    portal_url           TEXT NOT NULL DEFAULT '',
    status               TEXT NOT NULL DEFAULT '',
    shows_invoices       BOOLEAN NOT NULL DEFAULT false,
    shows_quotes         BOOLEAN NOT NULL DEFAULT false,
    shows_orders         BOOLEAN NOT NULL DEFAULT false,
    shows_delivery_notes BOOLEAN NOT NULL DEFAULT false,
    shows_credit_notes   BOOLEAN NOT NULL DEFAULT false,
    opened_on            DATE,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS finance.billable_time (
    id           UUID PRIMARY KEY,
    customer     TEXT NOT NULL DEFAULT '',
    description  TEXT NOT NULL DEFAULT '',
    division     TEXT NOT NULL DEFAULT '',
    hours        NUMERIC(10,2) NOT NULL DEFAULT 0,
    rate         NUMERIC(18,4) NOT NULL DEFAULT 0,
    amount       NUMERIC(18,4) NOT NULL DEFAULT 0,
    status       TEXT NOT NULL DEFAULT '',
    worked_on    DATE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
