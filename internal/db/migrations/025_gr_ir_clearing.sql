-- 025: Goods-Received / Invoice-Received (GR/IR) clearing.
--
-- Procurement now emits the monetary value on procurement.grn.posted and a poRef
-- on procurement.invoice.received. Finance accrues the AP liability at goods
-- receipt (Dr expense / Cr GR-IR clearing) and clears it against the GR-IR
-- account when the matching invoice arrives (Dr GR-IR clearing / Cr AP), instead
-- of booking the expense twice. grni_accruals tracks the open accrual per PO so
-- an invoice clears at most what was received.

INSERT INTO chart_of_accounts (code, name, account_type)
VALUES ('2150', 'GR/IR Clearing', 'liability')
ON CONFLICT (code) DO NOTHING;

CREATE TABLE IF NOT EXISTS grni_accruals (
    po_ref     TEXT PRIMARY KEY,
    currency   TEXT NOT NULL DEFAULT 'UGX',
    accrued    NUMERIC(18, 2) NOT NULL DEFAULT 0,
    cleared    NUMERIC(18, 2) NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
