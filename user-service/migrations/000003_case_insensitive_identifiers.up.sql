-- AI Assistance Disclosure:
-- Tool: Codex (GPT-5), date: 2026-09-28
-- Scope: Implements the recorded case-insensitive email and username indexes.
-- Author review: PENDING — reviewer to complete

ALTER TABLE users DROP CONSTRAINT users_email_key;
ALTER TABLE users DROP CONSTRAINT users_username_key;

CREATE UNIQUE INDEX idx_users_email_lower ON users (LOWER(email));
CREATE UNIQUE INDEX idx_users_username_lower ON users (LOWER(username));
