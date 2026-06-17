-- 027: finance fixed-asset subledger (cost, straight-line depreciation, NBV).
--
-- The physical asset master stays in iag-warehouse (wh_assets); this is the GL
-- subledger keyed to the warehouse asset tag (asset_ref). A monthly depreciation
-- run posts Dr Depreciation Expense / Cr Accumulated Depreciation; disposal
-- de-recognises cost + accumulated depreciation with the gain/loss. Accumulated
-- Depreciation is a contra-asset (credit balance) modelled under the 'asset'
-- type so it nets against gross fixed assets on the balance sheet.

INSERT INTO chart_of_accounts (code, name, account_type)
VALUES
    ('1510', 'Accumulated Depreciation', 'asset'),
    ('5300', 'Depreciation Expense', 'expense')
ON CONFLICT (code) DO NOTHING;

CREATE TABLE IF NOT EXISTS fa_assets (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    asset_ref                TEXT NOT NULL UNIQUE,
    description              TEXT NOT NULL DEFAULT '',
    category                 TEXT NOT NULL DEFAULT '',
    cost                     NUMERIC(18, 2) NOT NULL,
    salvage_value            NUMERIC(18, 2) NOT NULL DEFAULT 0,
    in_service_date          DATE NOT NULL,
    useful_life_months       INTEGER NOT NULL CHECK (useful_life_months > 0),
    method                   TEXT NOT NULL DEFAULT 'straight_line',
    accumulated_depreciation NUMERIC(18, 2) NOT NULL DEFAULT 0,
    currency                 TEXT NOT NULL DEFAULT 'UGX',
    status                   TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disposed')),
    created_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS fa_depreciation_entries (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    asset_id   UUID NOT NULL REFERENCES fa_assets(id),
    period     TEXT NOT NULL,
    amount     NUMERIC(18, 2) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (asset_id, period)
);
