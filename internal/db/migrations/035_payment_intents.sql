-- 035: Payment intents — a provider-agnostic record of an attempt to collect on
-- an AR open item. A concrete gateway (Pesapal/Flutterwave/MoMo) plugs in behind
-- the PaymentGateway interface; the bundled "manual" provider records an intent
-- and is settled by a webhook/confirm, reusing the existing payment path.
CREATE TABLE IF NOT EXISTS payment_intents (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    entity_id    UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000001' REFERENCES entities(id),
    open_item_id UUID NOT NULL REFERENCES ar_open_items(id),
    amount       NUMERIC(18, 2) NOT NULL CHECK (amount > 0),
    currency     TEXT NOT NULL DEFAULT 'UGX',
    provider     TEXT NOT NULL DEFAULT 'manual',
    status       TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'succeeded', 'failed')),
    external_ref TEXT,
    checkout_url TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_payment_intents_item ON payment_intents(open_item_id);
