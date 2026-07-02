-- 043: Three-way match — GR/IR variance & orphan detection
--
-- GR/IR clearing already nets goods-receipt accruals against vendor invoices in
-- either order, but nothing surfaced when the two sides disagreed (a price/qty
-- variance) or when an invoice cleared with no matching goods receipt (orphan).
-- This adds detection on top of the existing accruals, without changing the
-- order-independent clearing behaviour:
--   * Purchase Price Variance (5150) for writing a confirmed residual to P&L;
--   * match_status on grni_accruals (open|matched|variance|pending);
--   * match_exceptions: the review queue (orphan / over-invoice / price variance);
--   * match_tolerance: the allowed net difference as a fraction (default 2%).
--
-- Detection runs as a separate pass (/procurement/match-check); the clearing GL
-- is untouched. A confirmed variance can be written off to 5150 on request.

INSERT INTO chart_of_accounts (code, name, account_type) VALUES
    ('5150', 'Purchase Price Variance', 'expense')
ON CONFLICT (code) DO NOTHING;

ALTER TABLE grni_accruals ADD COLUMN IF NOT EXISTS match_status TEXT NOT NULL DEFAULT 'open';

CREATE TABLE IF NOT EXISTS match_exceptions (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    po_ref        TEXT NOT NULL,
    document_ref  TEXT NOT NULL DEFAULT '',
    type          TEXT NOT NULL
        CHECK (type IN ('orphan_invoice', 'over_invoice', 'price_variance', 'qty_variance')),
    detail        TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'resolved')),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_at   TIMESTAMPTZ,
    resolved_by   TEXT,
    UNIQUE (po_ref, type)
);
CREATE INDEX IF NOT EXISTS idx_match_exceptions_status ON match_exceptions(status);

CREATE TABLE IF NOT EXISTS match_tolerance (
    scope TEXT PRIMARY KEY,
    pct   NUMERIC(6, 4) NOT NULL DEFAULT 0.02 CHECK (pct >= 0)
);
INSERT INTO match_tolerance (scope, pct) VALUES ('default', 0.02)
ON CONFLICT (scope) DO NOTHING;
