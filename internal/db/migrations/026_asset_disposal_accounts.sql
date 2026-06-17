-- 026: chart-of-accounts entries for asset disposal booked from
-- warehouse.asset.disposed. Fixed Assets de-recognises the carrying amount; the
-- difference between proceeds and book value lands in gain/loss on disposal.
-- (A full fixed-asset register / depreciation is a separate, larger gap; until
-- then book value is carried on the disposal event.)

INSERT INTO chart_of_accounts (code, name, account_type)
VALUES
    ('1500', 'Fixed Assets', 'asset'),
    ('4200', 'Gain on Asset Disposal', 'revenue'),
    ('5200', 'Loss on Asset Disposal', 'expense')
ON CONFLICT (code) DO NOTHING;
