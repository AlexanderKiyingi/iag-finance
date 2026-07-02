-- 054: IAS 12 income taxes — current tax provision + deferred tax.
--
-- Current tax: on a taxable profit (the caller supplies it — permanent/temporary
-- adjustments to accounting profit are a judgement, not something to guess) at
-- the corporate rate (Uganda 30% default), book Dr 5700 Income Tax Expense /
-- Cr 2600 Income Tax Payable.
--
-- Deferred tax: on a temporary difference the caller tags deductible or taxable,
-- at the rate:
--   deductible difference -> deferred tax ASSET  : Dr 1700 / Cr 5700 (reduces expense)
--   taxable difference    -> deferred tax LIABILITY: Dr 5700 / Cr 2610
--
-- New accounts are also added to defaultAccounts.

INSERT INTO chart_of_accounts (code, name, account_type) VALUES
    ('1700', 'Deferred Tax Asset',     'asset'),
    ('2600', 'Income Tax Payable',     'liability'),
    ('2610', 'Deferred Tax Liability', 'liability'),
    ('5700', 'Income Tax Expense',     'expense')
ON CONFLICT (code) DO NOTHING;

CREATE TABLE IF NOT EXISTS income_tax_runs (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    entity_id        UUID,
    period           TEXT NOT NULL,                 -- label / idempotency key, e.g. '2026' or '2026-12'
    taxable_profit   NUMERIC(18, 2) NOT NULL,
    rate             NUMERIC(9, 6) NOT NULL,        -- fraction, e.g. 0.30
    tax_amount       NUMERIC(18, 2) NOT NULL,
    journal_entry_id UUID REFERENCES journal_entries(id) ON DELETE SET NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (period)
);

CREATE TABLE IF NOT EXISTS deferred_tax_items (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    entity_id        UUID,
    reference        TEXT NOT NULL,
    description      TEXT NOT NULL DEFAULT '',
    temp_difference  NUMERIC(18, 2) NOT NULL,
    dtype            TEXT NOT NULL CHECK (dtype IN ('deductible', 'taxable')),
    rate             NUMERIC(9, 6) NOT NULL,
    tax_amount       NUMERIC(18, 2) NOT NULL,
    journal_entry_id UUID REFERENCES journal_entries(id) ON DELETE SET NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (reference)
);
