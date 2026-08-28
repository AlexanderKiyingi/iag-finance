-- Bank reconciliations. The monolith records them; the platform modelled
-- nothing equivalent, so they had nowhere to go.
--
-- A reconciliation is a statement about a moment: on this date, the ledger said
-- one thing and the bank said another. The discrepancy is what makes it worth
-- recording, so it is a GENERATED column rather than a stored number - a stored
-- one can drift from the balances it claims to describe, and a reconciliation
-- whose discrepancy disagrees with its own figures is worse than none.
--
-- The sign convention is the source's: statement minus system. A negative
-- discrepancy means the bank shows less than the ledger does.


CREATE TABLE IF NOT EXISTS finance.bank_reconciliations (
    id                 UUID PRIMARY KEY,
    reference          TEXT NOT NULL UNIQUE,
    bank_account_code  TEXT NOT NULL REFERENCES finance.bank_accounts (code) ON DELETE RESTRICT,
    reconciled_on      DATE NOT NULL,
    system_balance     NUMERIC(18,4) NOT NULL DEFAULT 0,
    statement_balance  NUMERIC(18,4) NOT NULL DEFAULT 0,
    discrepancy        NUMERIC(18,4) GENERATED ALWAYS AS (statement_balance - system_balance) STORED,
    currency           TEXT NOT NULL DEFAULT 'UGX',
    status             TEXT NOT NULL DEFAULT 'Not reconciled',
    notes              TEXT NOT NULL DEFAULT '',
    created_by         TEXT NOT NULL DEFAULT '',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_bank_reconciliations_account ON finance.bank_reconciliations (bank_account_code, reconciled_on DESC);

