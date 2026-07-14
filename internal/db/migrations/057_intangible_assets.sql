-- 057: IAS 38 intangible-asset subledger. Mirrors the fixed-asset register (027)
-- for intangibles. Registration capitalizes Dr 1700 Intangible Assets / Cr
-- <source expense> (like PP&E's 1500 reclass); a future amortization run posts
-- Dr 5310 Amortization Expense / Cr 1710 Accumulated Amortization. Account codes
-- follow the asset-class convention already in the COA (1500 PP&E, 1600 ROU,
-- 1700 intangibles; 5300 depreciation / 5310 amortization expense).
INSERT INTO chart_of_accounts (code, name, account_type)
VALUES
    ('1700', 'Intangible Assets', 'asset'),
    ('1710', 'Accumulated Amortization', 'asset'),
    ('5310', 'Amortization Expense', 'expense')
ON CONFLICT (code) DO NOTHING;

CREATE TABLE IF NOT EXISTS ia_assets (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    asset_ref                TEXT NOT NULL UNIQUE,
    description              TEXT NOT NULL DEFAULT '',
    category                 TEXT NOT NULL DEFAULT '',
    cost                     NUMERIC(18, 2) NOT NULL,
    in_service_date          DATE NOT NULL,
    useful_life_months       INTEGER NOT NULL CHECK (useful_life_months > 0),
    accumulated_amortization NUMERIC(18, 2) NOT NULL DEFAULT 0,
    currency                 TEXT NOT NULL DEFAULT 'UGX',
    status                   TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disposed')),
    created_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
