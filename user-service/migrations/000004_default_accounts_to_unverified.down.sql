-- AI Assistance Disclosure:
-- Tool: Claude Code (model: Opus 5.5), date: 2026-10-17
-- Scope: Rollback for 000004; restores the ACTIVE default from 000001.
-- Author review: James Teo — reviewed and verified.

ALTER TABLE users ALTER COLUMN account_status SET DEFAULT 'ACTIVE';
