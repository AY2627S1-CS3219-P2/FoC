-- AI Assistance Disclosure:
-- Tool: Antigravity, date: 2026-10-20
-- Scope: Rollback reservation version column.
-- Author review: PENDING - reviewer to complete

ALTER TABLE credit_reservations DROP COLUMN IF EXISTS version;
