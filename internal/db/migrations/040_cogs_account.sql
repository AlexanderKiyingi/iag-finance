-- 040: Cost of Goods Sold control account — the finance-side foundation for
-- perpetual inventory (see docs/GAP_REMEDIATION_ROADMAP.md, "Blocked 3").
--
-- Valuation stays in iag-warehouse; finance books COGS from warehouse stock
-- events (Dr 5000 COGS / Cr 1400 Inventory on issue). This seeds the account so
-- that consumer wiring can land without a schema change. Inactive in the sense
-- that nothing posts to it until the warehouse events + consumer are enabled.
INSERT INTO chart_of_accounts (code, name, account_type)
VALUES ('5000', 'Cost of Goods Sold', 'expense')
ON CONFLICT (code) DO NOTHING;
