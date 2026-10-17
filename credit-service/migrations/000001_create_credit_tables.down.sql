-- AI Assistance Disclosure:
-- Tool: Codex (model: GPT-5), date: 2026-10-12
-- Scope: Transcribed owner-approved rollback for credit-service schema from docs/requirements.md Section 4.
-- Author review: PENDING - reviewer to complete

DROP TABLE IF EXISTS credit_transactions;
DROP TABLE IF EXISTS credit_reservations;
DROP TABLE IF EXISTS credit_accounts;
DROP TYPE IF EXISTS transaction_reason;
DROP TYPE IF EXISTS reservation_status;
