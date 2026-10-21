-- AI Assistance Disclosure:
-- Tool: Claude Code (model: Opus 5.5), date: 2026-10-18
-- Scope: Rollback for 000006; it deliberately runs no SQL.
-- Author review: James Teo — reviewed and verified.

-- No-op: the queued events stay in outbox_events. Once the relay has
-- published one, credit-service has acted on it and deleting the row cannot
-- undo that. The rows are also what stops a later re-run of 000006 from
-- queueing a second event for the same account.
