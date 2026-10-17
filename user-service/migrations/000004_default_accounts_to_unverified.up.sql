-- AI Assistance Disclosure:
-- Tool: Claude Code (model: Opus 5.5), date: 2026-10-17
-- Scope: Makes UNVERIFIED the default account_status for new users.
-- Author review: James Teo — reviewed and verified.

ALTER TABLE users ALTER COLUMN account_status SET DEFAULT 'UNVERIFIED';
