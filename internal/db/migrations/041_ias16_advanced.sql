-- 041: IAS 16 / IAS 36 — reducing-balance depreciation, impairment, revaluation
--
-- Depreciation was straight-line only (the fa_assets.method column existed but
-- was ignored) and there was no way to impair (IAS 36) or revalue (IAS 16
-- revaluation model) an asset. This adds:
--   * declining_rate on fa_assets — the annual reducing-balance rate, applied
--     monthly (NBV × rate / 12) when method='declining_balance';
--   * Impairment Loss (5310, expense) and Revaluation Surplus (3100, equity/OCI);
--   * fa_impairments and fa_revaluations subledgers with their GL links.
--
-- Straight-line assets are unaffected (method defaults to 'straight_line').

INSERT INTO chart_of_accounts (code, name, account_type) VALUES
    ('5310', 'Impairment Loss',     'expense'),
    ('3100', 'Revaluation Surplus', 'equity')
ON CONFLICT (code) DO NOTHING;

-- Annual reducing-balance rate (fraction, e.g. 0.25). NULL for straight-line.
ALTER TABLE fa_assets ADD COLUMN IF NOT EXISTS declining_rate NUMERIC(6, 4);

CREATE TABLE IF NOT EXISTS fa_impairments (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    asset_id         UUID NOT NULL REFERENCES fa_assets(id) ON DELETE CASCADE,
    effective_date   DATE NOT NULL,
    recoverable_amount NUMERIC(18, 2) NOT NULL DEFAULT 0,
    impairment_loss  NUMERIC(18, 2) NOT NULL,   -- positive = loss, negative = reversal
    is_reversal      BOOLEAN NOT NULL DEFAULT FALSE,
    journal_entry_id UUID REFERENCES journal_entries(id) ON DELETE SET NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_fa_impairments_asset ON fa_impairments(asset_id);

CREATE TABLE IF NOT EXISTS fa_revaluations (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    asset_id            UUID NOT NULL REFERENCES fa_assets(id) ON DELETE CASCADE,
    effective_date      DATE NOT NULL,
    new_carrying_amount NUMERIC(18, 2) NOT NULL,
    surplus_delta       NUMERIC(18, 2) NOT NULL,  -- positive = upward, negative = downward
    journal_entry_id    UUID REFERENCES journal_entries(id) ON DELETE SET NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_fa_revaluations_asset ON fa_revaluations(asset_id);
