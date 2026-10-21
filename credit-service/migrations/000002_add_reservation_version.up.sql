-- AI Assistance Disclosure:
-- Tool: Antigravity, date: 2026-10-20
-- Scope: Add version column to credit_reservations table to support outbox reconciliation (D-057).
-- Author review: PENDING - reviewer to complete

ALTER TABLE credit_reservations ADD COLUMN version BIGINT NOT NULL DEFAULT 1;
