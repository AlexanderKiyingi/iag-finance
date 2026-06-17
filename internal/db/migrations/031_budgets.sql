-- 031: Budgets (per entity, period, account) for budget-vs-actual reporting.
CREATE TABLE IF NOT EXISTS budgets (
    entity_id    UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000001' REFERENCES entities(id),
    period       TEXT NOT NULL,                 -- YYYY-MM
    account_code TEXT NOT NULL REFERENCES chart_of_accounts(code),
    amount       NUMERIC(18, 2) NOT NULL,       -- base currency
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (entity_id, period, account_code)
);
