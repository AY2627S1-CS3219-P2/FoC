-- AI Assistance Disclosure:
-- Tool: Claude Code (model: Opus 5.5), date: 2026-10-16
-- Scope: The outbox_events table that ActivateUser writes the user.registered
--   event to; columns and index mirror order-service's outbox.
-- Author review: James Teo — reviewed and verified.

-- One row per event, written in the same transaction as the change it
-- reports. The relay publishes due rows (available_at <= now()) and sets
-- published_at; a failed publish increments attempts, keeps last_error and
-- pushes available_at out by the retry delay.
CREATE TABLE outbox_events (
    event_id       UUID        PRIMARY KEY,
    routing_key    TEXT        NOT NULL,
    payload        JSONB       NOT NULL,
    correlation_id TEXT        NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    available_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at   TIMESTAMPTZ,
    attempts       INTEGER     NOT NULL DEFAULT 0,
    last_error     TEXT
);

-- Only unpublished rows are ever claimed, so the index holds only those.
CREATE INDEX outbox_events_due ON outbox_events (available_at) WHERE published_at IS NULL;
