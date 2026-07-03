-- 049: Quantity tracking for three-way match
--
-- GR/IR matching was value-only. This adds received/invoiced quantities to the
-- accrual so the detection pass can flag a quantity variance (goods received vs
-- invoiced) independently of price. Quantities default to 0 and are populated
-- only when the goods-receipt / invoice events carry them, so the check is inert
-- until upstream emits quantities — no behaviour change otherwise.

ALTER TABLE grni_accruals
    ADD COLUMN IF NOT EXISTS qty_received NUMERIC(18, 4) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS qty_invoiced NUMERIC(18, 4) NOT NULL DEFAULT 0;
