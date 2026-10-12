> Status: Owner-approved requirements for credit-service.
> Owner/reviewer: Seth
> Date: 2026-10-12

# Credit Service Specification

## 1. Domain Overview & Invariants

### Entities & Data Model
- **CreditAccount**:
    - `userId`: UUID (Primary Key / Unique)
    - `availableCredits`: Integer (Default: 100)
    - `reservedCredits`: Integer (Default: 0)
    - `version`: Integer (Optimistic concurrency / mutation token)
    - `updatedAt`: Timestamp
- **CreditReservation**:
    - `id`: UUID (Primary Key)
    - `errandId`: UUID (Unique, Indexed)
    - `requesterId`: UUID (Foreign Key -> CreditAccount.userId)
    - `amount`: Integer (> 0)
    - `status`: Enum (`RESERVED`, `COMMITTED`, `RELEASED`)
    - `createdAt`: Timestamp
    - `updatedAt`: Timestamp
- **CreditTransaction**:
    - `id`: UUID (Primary Key)
    - `userId`: UUID (Indexed)
    - `errandId`: UUID (Nullable, Indexed)
    - `amount`: Integer (Signed: positive for credits added, negative for deducted/reserved)
    - `reason`: Enum (`REGISTRATION_ALLOCATION`, `ERRAND_RESERVATION`, `ERRAND_CREATION_FAILED`, `ERRAND_COMPLETED_PAYOUT`, `ERRAND_COMPLETED_DEDUCTION`, `ERRAND_CANCELLED`, `ERRAND_EXPIRED`, `REWARD_INCREASED`, `REWARD_DECREASED`)
    - `createdAt`: Timestamp

### Core Invariants
1. **Non-negativity**: `availableCredits >= 0` and `reservedCredits >= 0` at all times enforced via DB check constraints.
2. **Auditability**: Every mutation to balances must append an immutable entry to `CreditTransaction`.
3. **Idempotency**: All notification consumers must check `CreditReservation.status` to safely ignore duplicate deliveries using `errandId`.
4. **Atomicity**: Multi-step balance updates must execute within a single ACID transaction with row-level locks (`SELECT ... FOR UPDATE`).
5. **Deadlock Prevention**: When locking multiple accounts (e.g., Requester and Courier during completion), always acquire row locks in deterministic order: `min(requesterId, courierId)` followed by `max(...)`.

---

## 2. Requirements Matrix

| ID | Description | Channel | Priority |
| :--- | :--- | :--- | :--- |
| **F4.1** | Allocate 100 credits to each user upon successful registration. | REST / Hook | High |
| **F4.1.1** | Create a credit balance for each newly registered user. | REST | High |
| **F4.1.2** | Initialise the user's available credit balance to 100 credits. | REST | High |
| **F4.1.3** | Record the initial credit allocation in the user's credit transaction history. | REST | Medium |
| **F4.2** | Reserve required credits from requester available balance upon errand request creation. | REST (`POST /reservations`) | High |
| **F4.2.1** | Reject reservation when requester has insufficient available credits. | REST | High |
| **F4.2.2** | Deduct reserved amount from requester available balance and transfer to reserved balance. | REST | High |
| **F4.2.3** | Transfer back reserved credits to available balance upon receiving errand creation failure notification. | AMQP (`errand.lifecycle.failed`) | High |
| **F4.3** | Transfer reserved credits to assigned courier upon receiving errand completion notification. | AMQP (`errand.lifecycle.completed`) | High |
| **F4.3.1** | Deduct errand reserved amount from requester reserved-credit balance. | AMQP | High |
| **F4.3.2** | Add errand reserved credit amount to assigned courier available-credit balance. | AMQP | High |
| **F4.3.3** | Ensure idempotency: do not change any credit balance on duplicate completion notifications. | AMQP | High |
| **F4.4** | Release reserved credits upon receiving cancellation or expiry notification for an errand. | AMQP (`errand.lifecycle.cancelled` / `.expired`) | High |
| **F4.4.1** | Deduct reserved amount from requester reserved balance and add back to available balance. | AMQP | High |
| **F4.4.2** | Ensure idempotency: ignore duplicate or retried cancellation/expiry notifications. | AMQP | High |
| **F4.5** | Adjust requester reserved credits when reward for an open errand is modified. | REST (`PATCH /reservations/:errand_id`) | Medium |
| **F4.5.1** | Reserve additional credits from available balance when reward increases. | REST | Medium |
| **F4.5.2** | Release difference to available balance when reward decreases. | REST | Medium |
| **F4.5.3** | Reject reward modification if requester has insufficient available credits for an increase. | REST | Medium |
| **F4.6** | Record comprehensive audit trail for every credit balance modification. | Database Ledger | Medium |
| **F4.6.1** | Record the reason for each balance change. | Database Ledger | Medium |
| **F4.6.2** | Record the numerical delta of credits changed. | Database Ledger | Medium |
| **F4.6.3** | Record the timestamp (date and time) of the balance change. | Database Ledger | Medium |
| **F4.6.4** | Associate unique `errandId` for all errand-related transactions. | Database Ledger | Medium |
| **F4.6.5** | Support non-errand balance updates with a `null` / optional `errandId`. | Database Ledger | Medium |
| **F4.7** | Expose authenticated read APIs for user credit account and history. | REST (`GET /accounts/me`) | Medium |
| **F4.7.1** | Return the user's available credit balance. | REST | Medium |
| **F4.7.2** | Return the user's reserved credit balance. | REST | Medium |
| **F4.7.3** | Return the paginated user credit transaction history. | REST | Medium |

---

## 3. RabbitMQ Messaging Architecture

### 3.1 Exchange & Queue Topology
- **Exchange**: `errand.events` (Topic Exchange, Durable)
- **Queue**: `credit-service.errand-events` (Durable)
    - Bound to:
        - `errand.lifecycle.completed`
        - `errand.lifecycle.cancelled`
        - `errand.lifecycle.expired`
        - `errand.lifecycle.failed`
    - Argument: `x-dead-letter-exchange: credit-service.dlx`
- **Dead-Letter Exchange (DLX)**: `credit-service.dlx` (Direct Exchange)
- **Dead-Letter Queue (DLQ)**: `credit-service.dlq` (Durable)

### 3.2 Reliability & Acknowledgment Rules
- **Manual ACKs (`autoAck: false`)**:
    - `Ack`: Sent only after DB transaction commits successfully or when an event is discarded as an idempotent duplicate.
    - `Nack(requeue: false)`: Sent on transient DB errors to route the message to the DLQ.

---

## 4. Database Schema (PostgreSQL DDL)

Save under `migrations/000001_create_credit_tables.up.sql`:

```sql
-- 1. Enums
CREATE TYPE reservation_status AS ENUM (
    'RESERVED',
    'COMMITTED',
    'RELEASED'
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

-- 2. Credit Accounts (User Aggregate)
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

-- 3. Credit Reservations (Errand Holds)
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

-- 4. Credit Transactions (Append-Only Ledger)
CREATE TABLE credit_transactions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES credit_accounts(user_id),
    errand_id UUID,
    amount INTEGER NOT NULL,
    reason transaction_reason NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_transactions_user_created ON credit_transactions(user_id, created_at DESC);
CREATE INDEX idx_transactions_