CREATE TABLE IF NOT EXISTS schema_migrations (
    version TEXT PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS auth_accounts (
    email TEXT PRIMARY KEY,
    password_hash TEXT NOT NULL DEFAULT '',
    role TEXT NOT NULL,
    display_name TEXT NOT NULL,
    entity TEXT NOT NULL DEFAULT 'Africa Coffee Park'
);

CREATE TABLE IF NOT EXISTS finance_app_state (
    id SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    state JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_finance_state_updated ON finance_app_state (updated_at);
