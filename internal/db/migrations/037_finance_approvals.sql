-- 037: finance approvals workflow.
--
-- High-value manual journal entries and AP/AR payment disbursements above the
-- amount bands require tiered sign-off (distinct approvers, low-to-high) before
-- they post. The approval row carries the action params (payload); on the final
-- tier's approval the workflow executes — posting the draft journal or applying
-- the payment. Below the first band's min_amount nothing needs approval.

CREATE TABLE IF NOT EXISTS finance_approval_tiers (
    tier          INTEGER PRIMARY KEY,
    label         TEXT NOT NULL DEFAULT '',
    min_amount    NUMERIC(18, 2) NOT NULL DEFAULT 0,
    max_amount    NUMERIC(18, 2),
    required_perm TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS finance_approvals (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    target_type  TEXT NOT NULL CHECK (target_type IN ('journal', 'payment')),
    amount       NUMERIC(18, 2) NOT NULL,
    currency     TEXT NOT NULL DEFAULT 'UGX',
    payload      JSONB NOT NULL DEFAULT '{}',
    status       TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected', 'executed')),
    requested_by TEXT NOT NULL DEFAULT '',
    description  TEXT NOT NULL DEFAULT '',
    result_ref   TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    decided_at   TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS finance_approvals_status_idx ON finance_approvals (status);

CREATE TABLE IF NOT EXISTS finance_approval_decisions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    approval_id UUID NOT NULL REFERENCES finance_approvals(id) ON DELETE CASCADE,
    tier        INTEGER NOT NULL,
    actor       TEXT NOT NULL DEFAULT '',
    decision    TEXT NOT NULL,
    note        TEXT NOT NULL DEFAULT '',
    decided_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS finance_approval_decisions_idx ON finance_approval_decisions (approval_id);

CREATE UNIQUE INDEX IF NOT EXISTS uq_finance_approval_decisions_tier ON finance_approval_decisions (approval_id, tier) WHERE decision = 'approved';

INSERT INTO finance_approval_tiers (tier, label, min_amount, max_amount, required_perm)
VALUES
    (1, 'Supervisor', 1000000,  10000000, 'finance.approve_tier1'),
    (2, 'Manager',    10000000, 50000000, 'finance.approve_tier2'),
    (3, 'Director',   50000000, NULL,     'finance.approve_tier3')
ON CONFLICT (tier) DO NOTHING;
