-- Structured demo data for inbox / bank views (replaces seed_* table_rows).

INSERT INTO bank_accounts (code, name, institution, currency, balance, status_label, purpose) VALUES
('1110', 'MoMo Float', 'MTN MoMo', 'UGX', 678000000, 'DAILY', 'Farmer payouts'),
('1120', 'Operating', 'Stanbic UG', 'UGX', 1820000000, '98%', 'H2H bulk pay'),
('1130', 'USD Account', 'Stanbic UG', 'USD', 354000, '98%', 'Export receipts')
ON CONFLICT (code) DO UPDATE SET
    balance = EXCLUDED.balance,
    status_label = EXCLUDED.status_label,
    updated_at = NOW();

INSERT INTO ap_open_items (vendor_ref, document_ref, description, amount, currency, due_date, status) VALUES
('Mukwano Industries', 'INV-AP-2026-04412', '3-way matched invoice', 1790000, 'UGX', '2026-06-07', 'open'),
('UEDCL', 'INV-AP-2026-04411', 'Awaiting approval', 4200000, 'UGX', '2026-05-12', 'open'),
('NWSC', 'INV-AP-2026-04410', 'Utilities', 840000, 'UGX', '2026-05-15', 'open')
ON CONFLICT (document_ref) DO NOTHING;

INSERT INTO cherry_intake_lines (intake_code, farmer_name, qty_kg, amount_ugx, status_label) VALUES
('CI-09472', 'Tugume Bosco', 47.0, 166850, 'PAY NOW'),
('CI-09471', 'K. Asasira', 52.4, 170300, 'PAID'),
('CI-09470', 'M. Tumusiime', 38.2, 124150, 'PAID')
ON CONFLICT (intake_code) DO UPDATE SET
    status_label = EXCLUDED.status_label,
    updated_at = NOW();
