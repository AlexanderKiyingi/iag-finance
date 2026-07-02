-- 045: Reverse-charge VAT
--
-- Cross-border / domestic reverse-charge supplies shift VAT accounting to the
-- buyer, who self-assesses BOTH output VAT (2100) and recoverable input VAT
-- (1300) on the same amount — a net-zero cash effect — instead of the supplier
-- charging it. This flags a tax code as reverse-charge; the self-assessment
-- endpoint books the offsetting pair.

ALTER TABLE tax_codes ADD COLUMN IF NOT EXISTS reverse_charge BOOLEAN NOT NULL DEFAULT FALSE;

INSERT INTO tax_codes (code, name, rate, active, reverse_charge) VALUES
    ('RC', 'Reverse Charge (18%)', 0.18, TRUE, TRUE)
ON CONFLICT (code) DO UPDATE SET reverse_charge = TRUE;
