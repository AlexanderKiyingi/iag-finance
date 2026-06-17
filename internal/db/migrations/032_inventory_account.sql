-- 032: Inventory control account (perpetual-inventory GL — foundation).
--
-- Valuation stays in iag-warehouse; finance books the GL from warehouse events.
-- Full perpetual inventory (Dr 1400 Inventory on receipt, Dr 5000 COGS / Cr 1400
-- on issue) requires switching procurement GR/IR from periodic expense to
-- inventory capitalisation — a coordinated cross-service change. This adds the
-- control account so that work can land without a schema change. See
-- docs/EVENT_CONTRACT.md (perpetual inventory).
INSERT INTO chart_of_accounts (code, name, account_type)
VALUES ('1400', 'Inventory', 'asset')
ON CONFLICT (code) DO NOTHING;
