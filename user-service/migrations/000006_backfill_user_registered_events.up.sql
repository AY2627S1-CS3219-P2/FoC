-- AI Assistance Disclosure:
-- Tool: Claude Code (model: Opus 5.5), date: 2026-10-18
-- Scope: Queues one user.registered event for every ACTIVE STUDENT account
--   that has none, for accounts activated before ActivateUser queued it.
-- Author review: James Teo — reviewed and verified.

-- The rows match what ActivateUser writes: a fresh UUID that is both the
-- row's event_id and the payload's eventId, and the payload fields eventId,
-- eventType, userId and occurredAt. The activation time of these accounts was
-- never recorded, so occurredAt is an RFC 3339 UTC timestamp (the account's
-- date_created), like the producer's.
--
-- NOT EXISTS keeps this to one event per account: an account that already
-- has a user.registered row, from ActivateUser or from an earlier run of this
-- statement, is skipped. ActivateUser writes the ACTIVE status and the event
-- in one transaction, so this statement sees both or neither; an activation
-- still in flight is UNVERIFIED here and queues its own event. Concurrent
-- runs of this migration are serialised by golang-migrate's advisory lock.
--
-- The CTE is MATERIALIZED so gen_random_uuid() runs once per account and the
-- same value lands in event_id and in the payload.
WITH backfill AS MATERIALIZED (
    SELECT gen_random_uuid() AS event_id, u.uid, u.date_created
    FROM users u
    WHERE u.account_role = 'STUDENT'
      AND u.account_status = 'ACTIVE'
      AND NOT EXISTS (
          SELECT 1
          FROM outbox_events o
          WHERE o.routing_key = 'user.registered'
            AND o.payload->>'userId' = u.uid::text
      )
)
INSERT INTO outbox_events (event_id, routing_key, payload)
SELECT
    event_id,
    'user.registered',
    jsonb_build_object(
        'eventId', event_id,
        'eventType', 'user.registered',
        'userId', uid,
        'occurredAt', to_char(date_created AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"')
    )
FROM backfill;
