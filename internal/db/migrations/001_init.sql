-- IAG Finance — initial schema (tenant-scoped for future multi-org)

CREATE TABLE IF NOT EXISTS audit_events (
    id           BIGSERIAL PRIMARY KEY,
    tenant_id    TEXT NOT NULL DEFAULT 'default',
    occurred_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    actor        TEXT NOT NULL,
    event_type   TEXT NOT NULL,
    message      TEXT NOT NULL,
    prev_hash    TEXT NOT NULL,
    event_hash   TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_audit_tenant_time
    ON audit_events (tenant_id, occurred_at DESC);

CREATE TABLE IF NOT EXISTS table_rows (
    id          BIGSERIAL PRIMARY KEY,
    tenant_id   TEXT NOT NULL DEFAULT 'default',
    table_id    TEXT NOT NULL,
    row_html    TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_table_rows_lookup
    ON table_rows (tenant_id, table_id, id);
