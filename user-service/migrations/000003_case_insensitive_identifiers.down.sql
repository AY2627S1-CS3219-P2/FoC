-- AI Assistance Disclosure:
-- Tool: Codex (GPT-5), date: 2026-09-28
-- Scope: Restores the exact-match identifier constraints when rolling back the recorded indexes.
-- Author review: PENDING — reviewer to complete

DROP INDEX idx_users_username_lower;
DROP INDEX idx_users_email_lower;

ALTER TABLE users ADD CONSTRAINT users_email_key UNIQUE (email);
ALTER TABLE users ADD CONSTRAINT users_username_key UNIQUE (username);
