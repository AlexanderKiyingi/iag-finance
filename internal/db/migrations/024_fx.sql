-- 024: Multi-currency (FX) support
--
-- Payments/adjustments previously accepted a mismatched currency and booked it
-- 1:1, and reports summed mixed currencies as raw numbers. This adds a base
-- currency, per-line transaction currency + base-currency equivalents, and a
-- per-document FX rate so each document's GL is booked at one rate (historical
-- method): base debits always equal base credits, and reports aggregate in the
-- base currency. Existing rows are base-currency UGX at rate 1, so single-
-- currency behaviour is unchanged.

CREATE TABLE IF NOT EXISTS exchange_rates (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    currency      TEXT NOT NULL,
    base_currency TEXT NOT NULL,
    rate          NUMERIC(18, 8) NOT NULL CHECK (rate > 0),
    as_of_date    DATE NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (currency, base_currency, as_of_date)
);
CREATE INDEX IF NOT EXISTS idx_exchange_rates_lookup
    ON exchange_rates (currency, base_currency, as_of_date DESC);

-- Per-line transaction currency + base-currency amounts.
ALTER TABLE journal_lines
    ADD COLUMN IF NOT EXISTS currency    TEXT NOT NULL DEFAULT 'UGX',
    ADD COLUMN IF NOT EXISTS debit_base  NUMERIC(18, 2) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS credit_base NUMERIC(18, 2) NOT NULL DEFAULT 0;

-- Backfill: existing lines are base-currency, rate 1 → base equals nominal.
UPDATE journal_lines SET debit_base = debit, credit_base = credit
WHERE debit_base = 0 AND credit_base = 0 AND (debit <> 0 OR credit <> 0);

-- Per-document FX rate captured at creation, so a document's invoice and its
-- later payments all convert to base at the same rate (books stay balanced).
ALTER TABLE ar_open_items
    ADD COLUMN IF NOT EXISTS fx_rate NUMERIC(18, 8) NOT NULL DEFAULT 1;
ALTER TABLE ap_open_items
    ADD COLUMN IF NOT EXISTS fx_rate NUMERIC(18, 8) NOT NULL DEFAULT 1;
