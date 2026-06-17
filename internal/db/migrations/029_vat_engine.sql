-- 029: VAT/GST engine — separate input VAT + configurable tax codes.
--
-- Input (purchase) VAT was debited to the same 2100 control as output (sales)
-- VAT, so the two could not be reported separately and the net VAT payable was
-- ambiguous. Add a dedicated recoverable Input VAT asset (1300); output VAT stays
-- in 2100. A tax_codes master makes rates configurable instead of hard-coded.

INSERT INTO chart_of_accounts (code, name, account_type)
VALUES ('1300', 'Input VAT', 'asset')
ON CONFLICT (code) DO NOTHING;

CREATE TABLE IF NOT EXISTS tax_codes (
    code       TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    rate       NUMERIC(6, 4) NOT NULL CHECK (rate >= 0),   -- 0.1800 = 18%
    active     BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Uganda defaults. The GL account is chosen by direction at booking time
-- (sales → 2100 output VAT, purchases → 1300 input VAT); the code carries the rate.
INSERT INTO tax_codes (code, name, rate) VALUES
    ('STD18', 'Standard-rated (18%)', 0.18),
    ('ZERO',  'Zero-rated (0%)',      0),
    ('EXEMPT','Exempt',               0)
ON CONFLICT (code) DO NOTHING;
