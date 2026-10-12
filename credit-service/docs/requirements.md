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
    - `status`: Enum (`RESERVED`, `PAID_OUT`, `REFUNDED`)
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
| **F4.1** | Allocate 100 credits to each user upon successful registration. | Async broker (registration event) | High |
| **F4.1.1** | Create a credit balance for each newly registered user. | Async broker | High |
| **F4.1.2** | Initialise the user's available credit balance to 100 credits. | Async broker | High |
| **F4.1.3** | Record the initial credit allocation in the user's credit transaction history. | Async broker | Medium |
| **F4.2** | Reserve required credits from requester available balance upon errand request creation. | REST (`POST /reservations`) | High |
| **F4.2.1** | Reject reservation when requester has insufficient available credits. | REST | High |
| **F4.2.2** | Deduct reserved amount from requester available balance and transfer to reserved balance. | REST | High |
| **F4.2.3** | Transfer back reserved credits to available balance upon receiving errand creation failure notification. | Async broker (`errand.lifecycle.failed`) | High |
| **F4.3** | Transfer reserved credits to assigned courier upon receiving errand completion notification. | Async broker (`errand.lifecycle.completed`) | High |
| **F4.3.1** | Deduct errand reserved amount from requester reserved-credit balance. | Async broker | High |
| **F4.3.2** | Add errand reserved credit amount to assigned courier available-credit balance. | Async broker | High |
| **F4.3.3** | Ensure idempotency: do not change any credit balance on duplicate completion notifications. | Async broker | High |
| **F4.4** | Release reserved credits upon receiving cancellation or expiry notification for an errand. | Async broker (`errand.lifecycle.cancelled` / `.expired`) | High |
| **F4.4.1** | Deduct reserved amount from requester reserved balance and add back to available balance. | Async broker | High |
| **F4.4.2** | Ensure idempotency: ignore duplicate or retried cancellation/expiry notifications. | Async broker | High |
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

## 3. Async Messaging Contract (Broker TBD)

<!-- AI-assisted clarification: broker-specific RabbitMQ/Kafka mechanics are intentionally deferred until the owner records the broker decision. -->

### 3.1 Broker-Agnostic Event Names
- Registration allocation event name: TBD by user-service and credit-service owners.
- `errand.lifecycle.completed`
- `errand.lifecycle.cancelled`
- `errand.lifecycle.expired`
- `errand.lifecycle.failed`

### 3.2 Required Event Payload Fields
Registration allocation events consumed by credit-service must include:
- `eventId`: UUID for broker/event-level observability.
- `userId`: UUID for the account receiving the initial allocation.
- `occurredAt`: UTC timestamp from the producing service.

All errand lifecycle events consumed by credit-service must include:
- `eventId`: UUID for broker/event-level observability.
- `errandId`: UUID used for credit idempotency through `credit_reservations.errand_id`.
- `requesterId`: UUID for the account whose credits were reserved.
- `amount`: integer credit amount, greater than zero.
- `occurredAt`: UTC timestamp from the producing service.

Completion events must additionally include:
- `courierId`: UUID for the account receiving the payout.

### 3.3 Consumer Reliability Rules
- A message is acknowledged only after the database transaction commits successfully.
- A duplicate event whose reservation is already in the target terminal state is acknowledged without mutating balances.
- A malformed event is rejected and routed to the broker's dead-letter mechanism once that mechanism is recorded.
- Transient database errors must not be acknowledged before the caller knows whether the transaction committed.

### 3.4 Broker-Specific Details Still Open
- Broker implementation: Kafka vs RabbitMQ.
- Topic, queue, exchange, consumer group, and dead-letter names.
- Retry policy and maximum delivery attempts.
- Serialization format and schema registry, if any.

---

## 4. Database Schema (PostgreSQL DDL)

Save under `migrations/000001_create_credit_tables.up.sql`:

```sql
-- 1. Enums
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
CREATE INDEX idx_transactions_errand ON credit_transactions(errand_id);
```

Save under `migrations/000001_create_credit_tables.down.sql`:

```sql
DROP TABLE IF EXISTS credit_transactions;
DROP TABLE IF EXISTS credit_reservations;
DROP TABLE IF EXISTS credit_accounts;
DROP TYPE IF EXISTS transaction_reason;
DROP TYPE IF EXISTS reservation_status;
```

---

## 5. Package & Folder Structure

<!-- AI-assisted clarification: this records implementation placement for the existing approved service layout without adding business behavior. -->

Credit-service follows the repository's service layering:
- `internal/credit`: domain models, repository interfaces, service methods, and sentinel errors.
- `internal/repository`: PostgreSQL adapter and transaction-bound persistence logic.
- `internal/httpapi`: HTTP DTOs, handlers, and Chi router.
- `internal/config`: environment loading.
- `internal/middleware`: identity propagation middleware after the team records that contract.
- `internal/gen`: generated OpenAPI code only; never hand-edited.
- `cmd/api`: composition root.
- `migrations`: reversible PostgreSQL migrations.

Broker adapter package naming is deferred until the broker decision is recorded.

---

## 6. Transaction & Locking Rules

<!-- AI-assisted clarification: restates the concurrency requirements in implementation-ready terms using the existing schema decisions. -->

- Every balance mutation must run in one PostgreSQL transaction.
- Repository methods that mutate an account must lock the relevant `credit_accounts` row with `SELECT ... FOR UPDATE`.
- Completion transfers that touch requester and courier accounts must lock both accounts in deterministic UUID order.
- Reservation creation must create or check the `credit_reservations` row in the same transaction as the requester account update.
- Reservation terminal transitions must check the current `credit_reservations.status` inside the same transaction as the balance updates.
- The service layer must not perform check-then-write balance logic outside the repository transaction boundary.

---

## 7. HTTP API Surface

The public HTTP contract is spec-first in `api/openapi.yaml`. The base path is
`/api/v1/credits`, and all endpoints use Bearer JWT security. Initial account
creation and credit allocation are intentionally excluded from the HTTP surface;
they are handled asynchronously from the registration allocation event in
Section 3.

| Method | Path | Purpose |
| :--- | :--- | :--- |
| `POST` | `/api/v1/credits/reservations` | Reserve requester credits for an errand. |
| `PATCH` | `/api/v1/credits/reservations/{errandId}` | Adjust the reserved reward for an open errand. |
| `GET` | `/api/v1/credits/accounts/me` | Return the authenticated user's available and reserved credits. |
| `GET` | `/api/v1/credits/accounts/me/transactions` | Return the authenticated user's paginated transaction history. |

### 7.1 Security
- Security scheme: `bearerAuth`, HTTP bearer JWT.
- Downstream services verify the raw Bearer JWT locally per D-041.

### 7.2 Error Envelope and Status Mapping
- Error response body: `{"error": "<message>"}`.
- `400 Bad Request`: syntax errors or invalid input, such as `amount <= 0`.
- `401 Unauthorized`: missing or invalid token.
- `404 Not Found`: entity not found.
- `409 Conflict`: duplicate reservation for `errandId`, or modifying a non-`RESERVED` hold.
- `422 Unprocessable Entity`: insufficient available credits.

### 7.3 Pagination
- `limit`: query integer, default `20`, minimum `1`, maximum `100`.
- `offset`: query integer, default `0`, minimum `0`.

### 7.4 Schemas
- `ErrorResponse`: `error` string.
- `AccountResponse`: `userId` UUID, `availableCredits` integer, `reservedCredits` integer, `updatedAt` date-time.
- `CreateReservationRequest`: `errandId` UUID, `amount` integer with minimum `1`.
- `AdjustRewardRequest`: `newReward` integer with minimum `1`.
- `ReservationResponse`: `id` UUID, `errandId` UUID, `requesterId` UUID, `amount` integer, `status` enum (`RESERVED`, `PAID_OUT`, `REFUNDED`), `createdAt` date-time, `updatedAt` date-time.
- `TransactionReason`: enum (`REGISTRATION_ALLOCATION`, `ERRAND_RESERVATION`, `ERRAND_CREATION_FAILED`, `ERRAND_COMPLETED_PAYOUT`, `ERRAND_COMPLETED_DEDUCTION`, `ERRAND_CANCELLED`, `ERRAND_EXPIRED`, `REWARD_INCREASED`, `REWARD_DECREASED`).
- `TransactionItem`: `id` UUID, `userId` UUID, nullable `errandId` UUID, `amount` integer, `reason` `TransactionReason`, `createdAt` date-time.
- `Pagination`: `limit` integer, `offset` integer, `total` integer.
- `TransactionListResponse`: `items` array of `TransactionItem`, `pagination` `Pagination`.

### 7.5 Endpoint Responses
- `POST /api/v1/credits/reservations`: `201 ReservationResponse`; errors `400`, `401`, `409`, `422`.
- `PATCH /api/v1/credits/reservations/{errandId}`: `200 ReservationResponse`; errors `400`, `401`, `404`, `409`, `422`.
- `GET /api/v1/credits/accounts/me`: `200 AccountResponse`; errors `401`, `404`.
- `GET /api/v1/credits/accounts/me/transactions`: `200 TransactionListResponse`; errors `400`, `401`.

---

## 8. Implementation Readiness Checklist

Ready now:
- Database schema migration and down migration from Section 4.
- Domain models and constants from Section 1.
- Repository transaction logic from Section 6.
- Service methods for registration allocation, reservation, reward adjustment, account read, and history read. Registration allocation must be callable from an async consumer adapter rather than an HTTP handler.

Blocked until further owner/team records:
- Broker adapter implementation.
- Broker-specific topology and retry/dead-letter behavior.
- Event consumer code that depends on exact broker library and delivery semantics.
