-- AI Assistance Disclosure:
-- Tool: Claude Code (model: Opus 5.5), date: 2026-10-17
-- Scope: Adds the UNVERIFIED value to the account_status enum that 000001 created.
-- Author review: James Teo — reviewed and verified.

-- PostgreSQL cannot use a new enum value in the transaction that adds it, so
-- the column default that uses it is set separately, in 000004.
ALTER TYPE account_status ADD VALUE IF NOT EXISTS 'UNVERIFIED';
