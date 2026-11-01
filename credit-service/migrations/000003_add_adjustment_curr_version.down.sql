-- AI Assistance Disclosure:
-- Tool: Codex (model: GPT-6), date: 2026-11-01
-- Scope: Roll back the reward adjustment order-version column.
-- Author review: Reviewed by Seth
ALTER TABLE credit_operation_results DROP CONSTRAINT chk_operation_results_curr_version_positive;
ALTER TABLE credit_operation_results DROP COLUMN curr_version;
