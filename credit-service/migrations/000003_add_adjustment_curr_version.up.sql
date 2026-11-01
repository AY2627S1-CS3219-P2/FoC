-- AI Assistance Disclosure:
-- Tool: Codex (model: GPT-6), date: 2026-11-01
-- Scope: Persist the order version bound to each idempotent reward adjustment.
-- Author review: Reviewed by Seth
ALTER TABLE credit_operation_results ADD COLUMN curr_version BIGINT NOT NULL DEFAULT 1;
ALTER TABLE credit_operation_results ADD CONSTRAINT chk_operation_results_curr_version_positive CHECK (curr_version > 0);
