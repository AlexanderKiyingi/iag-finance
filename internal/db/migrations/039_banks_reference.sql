-- 039: Bank reference list. Backs the "Bank Name" dropdown when creating a bank
-- account in the frontend (previously a hardcoded list in the SPA). It is a flat
-- reference list of licensed banks plus mobile-money wallets and petty cash —
-- not the bank *accounts* themselves (those live in chart-of-accounts / banking).
-- Global, not entity-scoped: the same list of payment institutions applies to
-- every entity. Read-only via GET /v1/banks; seeded here and editable by DBA.
CREATE TABLE IF NOT EXISTS banks (
    code       TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    -- 'bank' | 'mobile_money' | 'cash' — lets the UI group/sort the options.
    category   TEXT NOT NULL DEFAULT 'bank',
    sort_order INT  NOT NULL DEFAULT 100,
    active     BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Bank of Uganda licensed commercial banks + the two dominant mobile-money
-- wallets + petty cash. Idempotent: ON CONFLICT keeps an admin's later edits.
INSERT INTO banks (code, name, category, sort_order) VALUES
    ('ABSA',     'Absa Bank Uganda',                'bank',         10),
    ('BOA',      'Bank of Africa Uganda',           'bank',         20),
    ('BARODA',   'Bank of Baroda Uganda',           'bank',         30),
    ('BOU',      'Bank of Uganda',                  'bank',         40),
    ('CAIRO',    'Cairo Bank Uganda',               'bank',         50),
    ('CENTE',    'Centenary Bank',                  'bank',         60),
    ('DFCU',     'DFCU Bank',                       'bank',         70),
    ('DTB',      'Diamond Trust Bank Uganda',       'bank',         80),
    ('ECOBANK',  'Ecobank Uganda',                  'bank',         90),
    ('EQUITY',   'Equity Bank Uganda',              'bank',        100),
    ('FTB',      'Finance Trust Bank',              'bank',        110),
    ('HFB',      'Housing Finance Bank',            'bank',        120),
    ('IM',       'I&M Bank Uganda',                 'bank',        130),
    ('KCB',      'KCB Bank Uganda',                 'bank',        140),
    ('NCBA',     'NCBA Bank Uganda',                'bank',        150),
    ('POSTBANK', 'PostBank Uganda',                 'bank',        160),
    ('STANBIC',  'Stanbic Bank Uganda',             'bank',        170),
    ('SCB',      'Standard Chartered Bank Uganda',  'bank',        180),
    ('TROPICAL', 'Tropical Bank',                   'bank',        190),
    ('UBA',      'United Bank for Africa Uganda',   'bank',        200),
    ('AIRTEL',   'Airtel Money',                    'mobile_money', 300),
    ('MTN',      'MTN MoMo',                        'mobile_money', 310),
    ('PETTY',    'Petty Cash',                      'cash',         400)
ON CONFLICT (code) DO NOTHING;
