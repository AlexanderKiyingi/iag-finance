-- 056: Payments Clearing control account.
--
-- iag-payments emits payments.settled for every operational disbursement (coffee
-- payouts, vendor settlements, welfare loans, insurance claims, payroll, refunds).
-- The settlement consumer records the cash outflow here (Dr 1050 / Cr 1000) rather
-- than guessing the ultimate expense/asset/AP account. Finance then reconciles this
-- clearing account against the originating document (vendor invoice, payroll run,
-- loan agreement, claim). This avoids (a) misclassifying disbursements as COGS and
-- (b) double-booking against finance's native AP-payment / payroll GL paths.
--
-- coffee_payout is the one exception: it has no prior finance document, so it
-- capitalises straight to Inventory (1400).
INSERT INTO chart_of_accounts (code, name, account_type)
VALUES ('1050', 'Payments Clearing', 'asset')
ON CONFLICT (code) DO NOTHING;
