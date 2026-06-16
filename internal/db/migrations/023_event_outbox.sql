-- 023: Transactional outbox
--
-- Domain events were published fire-and-forget: if the broker was unavailable
-- the state change committed but the event was lost (e.g. an AP item created
-- without its invoice.posted). The outbox is written in the same transaction as
-- the state change; a relay worker delivers it at-least-once afterwards.
CREATE TABLE IF NOT EXISTS event_outbox (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    topic         TEXT NOT NULL,
    partition_key TEXT NOT NULL DEFAULT '',
    event_id      TEXT NOT NULL,
    event_type    TEXT NOT NULL,
    payload       JSONB NOT NULL DEFAULT '{}',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    published_at  TIMESTAMPTZ,
    attempts      INT NOT NULL DEFAULT 0,
    last_error    TEXT NOT NULL DEFAULT ''
);

-- One outbox row per event id (idempotent enqueue).
CREATE UNIQUE INDEX IF NOT EXISTS uq_event_outbox_event_id ON event_outbox(event_id);
-- Fast scan of the unpublished backlog for the relay worker.
CREATE INDEX IF NOT EXISTS idx_event_outbox_unpublished ON event_outbox(created_at) WHERE published_at IS NULL;
