-- 058: withholding-tax-received subledger. When a customer settles an invoice
-- they may withhold tax and issue a WHT certificate; the withheld amount is a
-- recoverable asset (claimable against income tax) that partially settles the
-- receivable. Recording a receipt posts Dr 1150 Withholding Tax Recoverable /
-- Cr 1100 Accounts Receivable for the withheld amount.
INSERT INTO chart_of_accounts (code, name, account_type)
VALUES ('1150', 'Withholding Tax Recoverable', 'asset')
ON CONFLICT (code) DO NOTHING;

CREATE TABLE IF NOT EXISTS wht_receipts (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    certificate_ref   TEXT NOT NULL UNIQUE,
    customer          TEXT NOT NULL DEFAULT '',
    invoice_reference TEXT NOT NULL DEFAULT '',
    tax_authority     TEXT NOT NULL DEFAULT '',
    amount            NUMERIC(18, 2) NOT NULL CHECK (amount > 0),
    receipt_date      DATE NOT NULL,
    currency          TEXT NOT NULL DEFAULT 'UGX',
    notes             TEXT NOT NULL DEFAULT '',
    status            TEXT NOT NULL DEFAULT 'recorded',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
