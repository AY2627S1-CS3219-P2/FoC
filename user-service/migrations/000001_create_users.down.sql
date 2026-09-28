-- AI Assistance Disclosure:
-- Tool: Codex (GPT-5), date: 2026-09-18
-- Scope: Added the rollback for the recorded users table and account enum migration.
-- Author review: COMPLETED BY ZI YANG

DROP INDEX IF EXISTS idx_users_username_lower;
DROP INDEX IF EXISTS idx_users_email_lower;
DROP TABLE users;
DROP TYPE account_status;
DROP TYPE account_role;
