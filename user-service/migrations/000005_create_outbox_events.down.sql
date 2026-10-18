-- AI Assistance Disclosure:
-- Tool: Claude Code (model: Opus 5.5), date: 2026-10-16
-- Scope: Rollback for the outbox_events table; dropping it drops its index.
-- Author review: James Teo — reviewed and verified.

DROP TABLE IF EXISTS outbox_events;
