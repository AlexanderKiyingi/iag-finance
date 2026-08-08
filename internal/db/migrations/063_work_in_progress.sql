-- 063: Work in Progress control account.
--
-- Production consumption and output never reached the ledger. Stores emitted
-- both movements with an empty cost, and this service's inventory mapping
-- treated any unrecognised movement type as cost-neutral, so a full
-- mill-to-roast run left inventory value unchanged: raw material never left,
-- finished goods never arrived.
--
-- WIP is what makes the two halves of a production order balance. Consumption
-- moves value out of Inventory (1400) and into WIP; output moves it back the
-- other way. Without it, consumption would have to debit COGS directly, which
-- would expense material the moment it entered a machine and understate
-- inventory for the whole of a run.
--
-- Costing basis is material-only: output is valued at what its order consumed,
-- so a completed order clears WIP to zero. Labour and energy stay as period
-- expense. A conversion-rate or standard-cost model would post its rate into
-- this same account, so adopting one later does not move the account.
--
-- Residual WIP on a closed order is yield loss and is swept to COGS; a WIP
-- balance nobody clears is how a WIP account stops meaning anything.
INSERT INTO chart_of_accounts (code, name, account_type)
VALUES ('1450', 'Work in Progress', 'asset')
ON CONFLICT (code) DO NOTHING;
