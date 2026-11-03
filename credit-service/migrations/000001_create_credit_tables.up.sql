-- AI Assistance Disclosure:
-- Tool: Antigravity, date: 2026-11-03
-- Scope: Compact schema migration to 3 core tables (credit_accounts, credit_reservations, credit_transactions), reservation versioning, and immutable ledger trigger.
-- Author review: Reviewed by Seth

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
    version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_reservations_amount_range CHECK (amount >= 1 AND amount <= 1000000),
    CONSTRAINT chk_reservations_version_positive CHECK (version > 0)
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
    reservation_version BIGINT NULL,
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
