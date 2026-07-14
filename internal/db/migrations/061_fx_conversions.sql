-- 061: FX-conversion (treasury) subledger. Records currency conversions between
-- bank accounts so they persist. Record-only: no GL is posted here because bank
-- accounts are a separate operational table (not chart_of_accounts entries), so
-- a correct per-bank journal can't be built without a bank->COA mapping. The real
-- cash/GL impact of the two bank movements flows through bank-statement import +
-- reconciliation. A GL-posting version (Dr/Cr each bank's GL + realized FX
-- gain/loss to gain_loss_account) is a follow-up once banks map to GL accounts.
CREATE TABLE IF NOT EXISTS fx_conversions (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    conversion_ref    TEXT NOT NULL UNIQUE,
    from_account      TEXT NOT NULL DEFAULT '',
    from_currency     TEXT NOT NULL DEFAULT '',
    from_amount       NUMERIC(18, 2) NOT NULL DEFAULT 0,
    to_account        TEXT NOT NULL DEFAULT '',
    to_currency       TEXT NOT NULL DEFAULT '',
    exchange_rate     NUMERIC(20, 8) NOT NULL DEFAULT 0,
    converted_amount  NUMERIC(18, 2) NOT NULL DEFAULT 0,
    fees              NUMERIC(18, 2) NOT NULL DEFAULT 0,
    gain_loss_account TEXT NOT NULL DEFAULT '',
    conversion_date   DATE NOT NULL,
    notes             TEXT NOT NULL DEFAULT '',
    status            TEXT NOT NULL DEFAULT 'recorded',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
