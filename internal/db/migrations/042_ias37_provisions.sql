-- 042: IAS 37 — provisions, and IAS 16/37 decommissioning obligations
--
-- There was no way to recognise a provision (a present obligation of uncertain
-- timing/amount) or a decommissioning liability. This adds:
--   * Provisions (2400) and Decommissioning Provision (2410) liabilities,
--     Provision Expense (5500) and the discount-unwind Finance Cost (5510);
--   * liab_provisions: the provision register (estimate, optional discount rate
--     and expected settlement date, running carrying amount);
--   * provision_movements: the audit of recognition, discount unwind,
--     remeasurement, utilisation and reversal, each linked to its GL entry.
--
-- A general provision expenses on recognition (Dr 5500 / Cr 2400); a
-- decommissioning provision capitalises into the asset (Dr 1500 / Cr 2410).

INSERT INTO chart_of_accounts (code, name, account_type) VALUES
    ('2400', 'Provisions',                 'liability'),
    ('2410', 'Decommissioning Provision',  'liability'),
    ('5500', 'Provision Expense',          'expense'),
    ('5510', 'Finance Cost - Unwinding of Discount', 'expense')
ON CONFLICT (code) DO NOTHING;

CREATE TABLE IF NOT EXISTS liab_provisions (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    kind                TEXT NOT NULL DEFAULT 'general'
        CHECK (kind IN ('general', 'legal', 'warranty', 'decommissioning')),
    description         TEXT NOT NULL DEFAULT '',
    estimate            NUMERIC(18, 2) NOT NULL CHECK (estimate >= 0),
    discount_rate       NUMERIC(6, 4) NOT NULL DEFAULT 0,
    expected_settlement DATE,
    carrying_amount     NUMERIC(18, 2) NOT NULL DEFAULT 0,
    currency            TEXT NOT NULL DEFAULT 'UGX',
    asset_ref           TEXT,                  -- decommissioning: the asset it attaches to
    entity_id           UUID,
    status              TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'settled', 'reversed')),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS provision_movements (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provision_id     UUID NOT NULL REFERENCES liab_provisions(id) ON DELETE CASCADE,
    effective_date   DATE NOT NULL,
    kind             TEXT NOT NULL
        CHECK (kind IN ('recognize', 'unwind', 'remeasure', 'utilize', 'reverse')),
    amount           NUMERIC(18, 2) NOT NULL,   -- signed movement to the carrying amount
    journal_entry_id UUID REFERENCES journal_entries(id) ON DELETE SET NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_provision_movements_prov ON provision_movements(provision_id);
