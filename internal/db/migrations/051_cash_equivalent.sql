-- 048: Cash & cash-equivalents flag on chart_of_accounts.
--
-- The direct-method cash-flow statement previously identified "the cash account"
-- by the literal code '1000'. That silently undercounts once cash moves through
-- any other cash/bank GL account. Make it data-driven: an account opts in as a
-- cash-equivalent and the statement keys on the flag, not a magic code.
--
-- Seeded 1000 Cash is flagged here; SeedChartOfAccounts also sets it so the
-- behaviour is identical on a fresh database regardless of seed/migration order.

ALTER TABLE chart_of_accounts
    ADD COLUMN IF NOT EXISTS is_cash_equivalent BOOLEAN NOT NULL DEFAULT FALSE;

UPDATE chart_of_accounts SET is_cash_equivalent = TRUE WHERE code = '1000';
