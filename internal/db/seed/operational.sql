-- Structured demo data for inbox / bank views (replaces seed_* table_rows).

INSERT INTO bank_accounts (tenant_id, code, name, institution, currency, balance, status_label, purpose) VALUES
('default', '1110', 'MoMo Float', 'MTN MoMo', 'UGX', 678000000, 'DAILY', 'Farmer payouts'),
('default', '1120', 'Operating', 'Stanbic UG', 'UGX', 1820000000, '98%', 'H2H bulk pay'),
('default', '1130', 'USD Account', 'Stanbic UG', 'USD', 354000, '98%', 'Export receipts')
ON CONFLICT (tenant_id, code) DO UPDATE SET
    balance = EXCLUDED.balance,
    status_label = EXCLUDED.status_label,
    updated_at = NOW();

INSERT INTO ap_open_items (tenant_id, vendor_ref, document_ref, description, amount, currency, due_date, status) VALUES
('default', 'Mukwano Industries', 'INV-AP-2026-04412', '3-way matched invoice', 1790000, 'UGX', '2026-06-07', 'open'),
('default', 'UEDCL', 'INV-AP-2026-04411', 'Awaiting approval', 4200000, 'UGX', '2026-05-12', 'open'),
('default', 'NWSC', 'INV-AP-2026-04410', 'Utilities', 840000, 'UGX', '2026-05-15', 'open')
ON CONFLICT (document_ref) DO NOTHING;

INSERT INTO cherry_intake_lines (tenant_id, intake_code, farmer_name, qty_kg, amount_ugx, status_label) VALUES
('default', 'CI-09472', 'Tugume Bosco', 47.0, 166850, 'PAY NOW'),
('default', 'CI-09471', 'K. Asasira', 52.4, 170300, 'PAID'),
('default', 'CI-09470', 'M. Tumusiime', 38.2, 124150, 'PAID')
ON CONFLICT (tenant_id, intake_code) DO UPDATE SET
    status_label = EXCLUDED.status_label,
    updated_at = NOW();
