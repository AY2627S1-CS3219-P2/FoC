-- AI Assistance Disclosure:
-- Tool: Claude Code (model: Opus 5.5), date: 2026-10-17
-- Scope: Rollback for 000003; it deliberately runs no SQL.
-- Author review: James Teo — reviewed and verified.

-- No-op: PostgreSQL cannot drop a value from an enum type, so UNVERIFIED stays
-- in account_status after this migration is rolled back.
