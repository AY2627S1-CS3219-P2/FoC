-- AI Assistance Disclosure:
-- Tool: Antigravity, date: 2026-11-03
-- Scope: Reversible down migration dropping all 3 core credit tables, append-only trigger, and 3 enums.
-- Author review: Reviewed by Seth

DROP TRIGGER IF EXISTS credit_transactions_immutable ON credit_transactions;
DROP FUNCTION IF EXISTS prevent_credit_transaction_changes();

DROP TABLE IF EXISTS credit_transactions;
DROP TABLE IF EXISTS credit_reservations;
DROP TABLE IF EXISTS credit_accounts;

DROP TYPE IF EXISTS account_integrity_status;
DROP TYPE IF EXISTS transaction_reason;
DROP TYPE IF EXISTS reservation_status;
