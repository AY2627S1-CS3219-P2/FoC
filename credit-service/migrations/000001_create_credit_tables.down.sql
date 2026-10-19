-- AI Assistance Disclosure:
-- Tool: Antigravity, date: 2026-10-19
-- Scope: Reversible down migration dropping all 6 credit tables, append-only trigger, and 5 enums.
-- Author review: PENDING - reviewer to complete

DROP TRIGGER IF EXISTS credit_transactions_immutable ON credit_transactions;
DROP FUNCTION IF EXISTS prevent_credit_transaction_changes();

DROP TABLE IF EXISTS pending_payouts;
DROP TABLE IF EXISTS pending_refunds;
DROP TABLE IF EXISTS credit_operation_results;
DROP TABLE IF EXISTS credit_transactions;
DROP TABLE IF EXISTS credit_reservations;
DROP TABLE IF EXISTS credit_accounts;

DROP TYPE IF EXISTS credit_operation_outcome;
DROP TYPE IF EXISTS credit_operation_type;
DROP TYPE IF EXISTS account_integrity_status;
DROP TYPE IF EXISTS transaction_reason;
DROP TYPE IF EXISTS reservation_status;
