-- 050: Recognition spec on recurring invoices (IFRS 15 for subscriptions)
--
-- A recurring schedule can now carry a ratable recognition method + period count.
-- Each invoice the worker generates inherits it, so every period's invoice defers
-- its revenue and spreads it over recognition_periods months from its own issue —
-- the standard treatment for a subscription billed in advance.
--
-- Empty method (default) keeps point-in-time recognition for existing schedules.

ALTER TABLE recurring_invoices
    ADD COLUMN IF NOT EXISTS recognition_method  TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS recognition_periods INT  NOT NULL DEFAULT 0;
