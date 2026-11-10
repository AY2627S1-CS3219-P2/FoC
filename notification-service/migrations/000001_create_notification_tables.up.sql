-- AI Assistance Disclosure:
-- Tool: Claude Code (model: Opus 5.5), date: 2026-11-05
-- Scope: Transcribes the stored notification (backlog F5.1.1, D-146), with the
--   event id, message and current courier D-146 adds.
-- Author review: nigeltzy - checked the migration sequence and commands, expected.

CREATE TABLE notifications (
    id           uuid PRIMARY KEY,
    recipient_id uuid NOT NULL,
    errand_id    uuid NOT NULL,
    event_id     uuid NOT NULL,
    state_change text NOT NULL,
    message      text NOT NULL,
    is_read      boolean NOT NULL DEFAULT false,
    created_at   timestamptz NOT NULL DEFAULT now(),
    -- At most one per recipient per event, however often it is redelivered
    -- (F5.5.2, F6.1.2).
    CONSTRAINT notifications_one_per_recipient_per_event UNIQUE (event_id, recipient_id)
);

-- A user's list, newest first (F5.1.3).
CREATE INDEX notifications_recipient_newest ON notifications (recipient_id, created_at DESC, id DESC);

-- The courier now on each errand, because errand.cancelled does not say (D-146).
-- changed_at is the event's own time, so an older event arriving late cannot
-- overwrite a newer one. courier_id is NULL after a withdrawal.
CREATE TABLE errand_couriers (
    errand_id  uuid PRIMARY KEY,
    courier_id uuid,
    changed_at timestamptz NOT NULL
);
