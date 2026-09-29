-- AI Assistance Disclosure:
-- Tool: Codex (GPT-5), date: 2026-09-19
-- Scope: Added the rollback for the refresh-session table.
-- Author review: COMPLETED BY ZI YANG

-- AI Assistance Disclosure:
-- Tool: Codex (GPT-5), date: 2026-09-28
-- Scope: Corrected the stale scope; this rollback removes only the refresh-session table.
-- Author review: ZI YANG - verified correctness

DROP TABLE sessions;
