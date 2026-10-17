-- AI Assistance Disclosure:
-- Tool: Codex (model: GPT-5), date: 2026-10-12
-- Scope: Transcribed owner-approved credit-service schema from docs/requirements.md Section 4.
-- Author review: PENDING - reviewer to complete

CREATE TYPE reservation_status AS ENUM (
    'RESERVED',
    'PAID_OUT',
    'REFUNDED'
);

CREATE TYPE transaction_reason AS ENUM (
    'REGISTRATION_ALLOCATION',
    'ERRAND_RESERVATION',
    'ERRAND_CREATION_FAILED',
    'ERRAND_COMPLETED_PAYOUT',
    'ERRAND_COMPLETED_DEDUCTION',
    'ERRAND_CANCELLED',
    'ERRAND_EXPIRED',
    'REWARD_INCREASED',
    'REWARD_DECREASED'
);

CREATE TABLE credit_accounts (
    user_id UUID PRIMARY KEY,
    available_credits INTEGER NOT NULL DEFAULT 100,
    reserved_credits INTEGER NOT NULL DEFAULT 0,
    version INTEGER NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_available_non_negative CHECK (available_credits >= 0),
    CONSTRAINT chk_reserved_non_negative CHECK (reserved_credits >= 0)
);

CREATE TABLE credit_reservations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    errand_id UUID NOT NULL,
    requester_id UUID NOT NULL REFERENCES credit_accounts(user_id),
    amount INTEGER NOT NULL,
    status reservation_status NOT NULL DEFAULT 'RESERVED',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_reservation_amount_positive CHECK (amount > 0),
    CONSTRAINT uq_errand_reservation UNIQUE (errand_id)
);

CREATE INDEX idx_reservations_requester ON credit_reservations(requester_id);

CREATE TABLE credit_transactions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES credit_accounts(user_id),
    errand_id UUID,
    amount INTEGER NOT NULL,
    reason transaction_reason NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_transactions_user_created ON credit_transactions(user_id, created_at DESC);
CREATE INDEX idx_transactions_errand ON credit_transactions(errand_id);
