-- 059: late-fee (interest/penalty) subledger. A late fee charged on an overdue
-- invoice increases the customer's receivable and is recognised as finance income:
-- Dr 1100 Accounts Receivable / Cr 4300 Late Fee & Interest Income.
INSERT INTO chart_of_accounts (code, name, account_type)
VALUES ('4300', 'Late Fee & Interest Income', 'revenue')
ON CONFLICT (code) DO NOTHING;

CREATE TABLE IF NOT EXISTS late_fees (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    fee_ref           TEXT NOT NULL UNIQUE,
    customer          TEXT NOT NULL DEFAULT '',
    invoice_reference TEXT NOT NULL DEFAULT '',
    rate              NUMERIC(9, 4) NOT NULL DEFAULT 0,
    amount            NUMERIC(18, 2) NOT NULL CHECK (amount > 0),
    fee_date          DATE NOT NULL,
    currency          TEXT NOT NULL DEFAULT 'UGX',
    notes             TEXT NOT NULL DEFAULT '',
    status            TEXT NOT NULL DEFAULT 'charged',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
