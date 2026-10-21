-- AI Assistance Disclosure:
-- Tool: Antigravity, date: 2026-10-19
-- Scope: Finalized initial database schema migration with 6 tables, 5 enums, 64-bit bounds, integrity fields, and append-only trigger.
-- Author review: PENDING - reviewer to complete

-- 1. Enums
CREATE TYPE reservation_status AS ENUM (
    'RESERVED',
    'PAID_OUT',
    'REFUNDED'
);

CREATE TYPE transaction_reason AS ENUM (
    'REGISTRATION_ALLOCATION',
    'ERRAND_RESERVATION',
    'ERRAND_FAILED',
    'ERRAND_COMPLETED_PAYOUT',
    'ERRAND_COMPLETED_DEDUCTION',
    'ERRAND_CANCELLED',
    'ERRAND_EXPIRED',
    'REWARD_INCREASED',
    'REWARD_DECREASED'
);

CREATE TYPE account_integrity_status AS ENUM (
    'CONSISTENT',
    'FLAGGED'
);

CREATE TYPE credit_operation_type AS ENUM (
    'REWARD_ADJUSTMENT'
);

CREATE TYPE credit_operation_outcome AS ENUM (
    'SUCCESS',
    'WALLET_NOT_READY',
    'INSUFFICIENT_CREDITS',
    'RESERVATION_STATE_CONFLICT',
    'RESERVATION_AMOUNT_MISMATCH',
    'ACCOUNT_INTEGRITY_FAILURE',
    'CREDIT_LIMIT_EXCEEDED'
);

-- 2. Credit Accounts (User Aggregate)
CREATE TABLE credit_accounts (
    user_id UUID PRIMARY KEY,
    available_credits BIGINT NOT NULL DEFAULT 0,
    reserved_credits BIGINT NOT NULL DEFAULT 0,
    version BIGINT NOT NULL DEFAULT 1,
    integrity_status account_integrity_status NOT NULL DEFAULT 'CONSISTENT',
    integrity_flagged_at TIMESTAMPTZ NULL,
    integrity_reason_code TEXT NULL,
    integrity_detail VARCHAR(512) NULL,
    integrity_expected NUMERIC NULL,
    integrity_observed NUMERIC NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_accounts_available_credits_range CHECK (available_credits >= 0 AND available_credits <= 1000000000),
    CONSTRAINT chk_accounts_reserved_credits_range CHECK (reserved_credits >= 0 AND reserved_credits <= 1000000000),
    CONSTRAINT chk_accounts_version_positive CHECK (version > 0),
    CONSTRAINT chk_accounts_integrity_fields CHECK (
        (integrity_status = 'CONSISTENT' AND integrity_flagged_at IS NULL AND integrity_reason_code IS NULL AND integrity_detail IS NULL AND integrity_expected IS NULL AND integrity_observed IS NULL)
        OR
        (integrity_status = 'FLAGGED' AND integrity_flagged_at IS NOT NULL AND integrity_reason_code IS NOT NULL)
    )
);

CREATE INDEX idx_accounts_flagged ON credit_accounts(integrity_flagged_at, user_id) WHERE integrity_status = 'FLAGGED';

-- 3. Credit Reservations (Errand Holds)
CREATE TABLE credit_reservations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    errand_id UUID NOT NULL UNIQUE,
    requester_id UUID NOT NULL REFERENCES credit_accounts(user_id),
    amount BIGINT NOT NULL,
    status reservation_status NOT NULL DEFAULT 'RESERVED',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_reservations_amount_range CHECK (amount >= 1 AND amount <= 1000000)
);

CREATE INDEX idx_reservations_requester ON credit_reservations(requester_id);

-- 4. Credit Transactions (Append-Only Ledger)
CREATE TABLE credit_transactions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES credit_accounts(user_id),
    errand_id UUID NULL REFERENCES credit_reservations(errand_id) ON DELETE RESTRICT,
    operation_id UUID NOT NULL,
    balance_bucket TEXT NOT NULL CHECK (balance_bucket IN ('AVAILABLE', 'RESERVED')),
    amount BIGINT NOT NULL,
    reason transaction_reason NOT NULL,
    occurred_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_transactions_amount CHECK (amount <> 0 AND amount >= -1000000 AND amount <= 1000000)
);

CREATE INDEX idx_transactions_user_created ON credit_transactions(user_id, created_at DESC, operation_id DESC);
CREATE INDEX idx_transactions_operation ON credit_transactions(operation_id);
CREATE INDEX idx_transactions_errand ON credit_transactions(errand_id);
CREATE UNIQUE INDEX uq_credit_transaction_operation_bucket ON credit_transactions(operation_id, user_id, balance_bucket);

-- Append-only trigger for credit_transactions
CREATE OR REPLACE FUNCTION prevent_credit_transaction_changes()
RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'Credit transactions are append-only';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER credit_transactions_immutable
BEFORE UPDATE OR DELETE ON credit_transactions
FOR EACH ROW
EXECUTE FUNCTION prevent_credit_transaction_changes();

-- 5. Credit Operation Results (Adjustment Idempotency)
CREATE TABLE credit_operation_results (
    operation_id UUID PRIMARY KEY,
    operation_type credit_operation_type NOT NULL,
    errand_id UUID NOT NULL REFERENCES credit_reservations(errand_id) ON DELETE RESTRICT,
    current_amount BIGINT NOT NULL,
    new_amount BIGINT NOT NULL,
    outcome_code credit_operation_outcome NOT NULL,
    resulting_amount BIGINT NULL,
    authoritative_amount BIGINT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_operation_results_current_amount CHECK (current_amount >= 1 AND current_amount <= 1000000),
    CONSTRAINT chk_operation_results_new_amount CHECK (new_amount >= 1 AND new_amount <= 1000000),
    CONSTRAINT chk_operation_results_resulting_amount CHECK (resulting_amount IS NULL OR (resulting_amount >= 1 AND resulting_amount <= 1000000)),
    CONSTRAINT chk_operation_results_authoritative_amount CHECK (authoritative_amount IS NULL OR (authoritative_amount >= 1 AND authoritative_amount <= 1000000)),
    CONSTRAINT chk_operation_results_amounts CHECK (
        (outcome_code = 'SUCCESS' AND resulting_amount IS NOT NULL AND authoritative_amount IS NULL)
        OR
        (outcome_code = 'RESERVATION_AMOUNT_MISMATCH' AND resulting_amount IS NULL AND authoritative_amount IS NOT NULL)
        OR
        (outcome_code NOT IN ('SUCCESS', 'RESERVATION_AMOUNT_MISMATCH') AND resulting_amount IS NULL AND authoritative_amount IS NULL)
    )
);

CREATE INDEX idx_operation_results_errand ON credit_operation_results(errand_id);

-- 6. Pending Refunds (Durable Blocked Refunds)
CREATE TABLE pending_refunds (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    errand_id UUID NOT NULL UNIQUE REFERENCES credit_reservations(errand_id) ON DELETE RESTRICT,
    event_id UUID NOT NULL UNIQUE,
    reason transaction_reason NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    attempt_count BIGINT NOT NULL DEFAULT 0,
    last_attempt_at TIMESTAMPTZ NULL,
    last_error VARCHAR(512) NULL,
    overdue_alerted_at TIMESTAMPTZ NULL,

    CONSTRAINT chk_pending_refunds_reason CHECK (reason IN ('ERRAND_CANCELLED', 'ERRAND_EXPIRED', 'ERRAND_FAILED')),
    CONSTRAINT chk_pending_refunds_attempt_count CHECK (attempt_count >= 0)
);

CREATE INDEX idx_pending_refunds_next_attempt ON pending_refunds(next_attempt_at);

-- 7. Pending Payouts (Durable Delayed Payouts)
CREATE TABLE pending_payouts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    errand_id UUID NOT NULL UNIQUE REFERENCES credit_reservations(errand_id) ON DELETE RESTRICT,
    event_id UUID NOT NULL UNIQUE,
    courier_id UUID NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    attempt_count BIGINT NOT NULL DEFAULT 0,
    last_attempt_at TIMESTAMPTZ NULL,
    blocked_reason TEXT NOT NULL,
    last_error VARCHAR(512) NULL,
    overdue_alerted_at TIMESTAMPTZ NULL,

    CONSTRAINT chk_pending_payouts_blocked_reason CHECK (blocked_reason IN ('MISSING_WALLET', 'ACCOUNT_FLAGGED', 'AVAILABLE_LIMIT_EXCEEDED')),
    CONSTRAINT chk_pending_payouts_attempt_count CHECK (attempt_count >= 0)
);

CREATE INDEX idx_pending_payouts_next_attempt ON pending_payouts(next_attempt_at);
