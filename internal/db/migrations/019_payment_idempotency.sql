-- 019: Payment idempotency backstop
--
-- A retried receipt/disbursement must not apply twice. Idempotency is enforced
-- in the booking transaction via processed_events, but a DB-level unique key on
-- (open_item_id, payment_ref) is the durable backstop: a duplicate (item, ref)
-- can never be inserted, even if application-level guards are bypassed.
CREATE UNIQUE INDEX IF NOT EXISTS uq_finance_payments_item_ref
    ON finance_payments (open_item_id, payment_ref);
