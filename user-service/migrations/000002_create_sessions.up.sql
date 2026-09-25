-- AI Assistance Disclosure:
-- Tool: Codex (GPT-5), date: 2026-09-19
-- Scope: Added the recorded token-validity field and refresh-session table.
-- Author review: COMPLETED BY ZI YANG

CREATE TABLE sessions (
    jti UUID PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    token_hash VARCHAR(255) NOT NULL UNIQUE,
    replaced_by_token_hash VARCHAR(255),
    uid UUID NOT NULL REFERENCES users(uid) ON DELETE CASCADE
);
