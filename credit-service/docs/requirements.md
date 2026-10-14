> Status: Revised draft for owner review (original decisions retained unless noted).
> Owner/reviewer: Seth
> Date: 2026-10-12

# Credit Service Specification

## 1. Domain Overview & Invariants

### Entities & Data Model
- **CreditAccount**:
  - `userId`: UUID (Primary Key / Unique)
  - `availableCredits`: Integer (initialised explicitly by the registration allocation operation)
  - `reservedCredits`: Integer (Default: 0)
  - `version`: Integer (incremented on each wallet mutation; reserved for optimistic concurrency where explicitly used)
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
  - `amount`: Integer (Signed delta to the identified balance bucket)
  - `balanceBucket`: Enum (`AVAILABLE`, `RESERVED`)
  - `operationId`: UUID (groups entries belonging to one atomic business operation)
  - `reversesTransactionId`: UUID (Nullable, links a compensating row to the original transaction it reverses)
  - `reason`: Enum (`REGISTRATION_ALLOCATION`, `ERRAND_RESERVATION`, `ERRAND_CREATION_FAILED`, `ERRAND_COMPLETED_PAYOUT`, `ERRAND_COMPLETED_DEDUCTION`, `ERRAND_CANCELLED`, `ERRAND_EXPIRED`, `REWARD_INCREASED`, `REWARD_DECREASED`)
  - `createdAt`: Timestamp

- Credit transaction idempotency is enforced per `(operationId, userId, balanceBucket)`.
- Ledger reversals are represented by new compensating transaction rows linked to
the original row through nullable `reversesTransactionId`.

### Core Invariants
1. **Non-negativity**: `availableCredits >= 0` and `reservedCredits >= 0` at all times enforced via DB check constraints.
2. **Auditability**: Every mutation to balances must append an immutable entry to `CreditTransaction`.
3. **Idempotency**: Each business operation shall have at most one financial effect. Identical retries shall return or acknowledge the prior outcome without repeating mutations; conflicting retries (same business identifier, different amount/requester/courier) shall be rejected and recorded for investigation. Registration allocation is unique per user; reservation creation is unique per `errandId`; terminal processing is guarded by the reservation state. `eventId` supports delivery diagnostics, not business-level uniqueness by itself.
4. **Atomicity**: Wallet changes, reservation creation/transition, ledger entries, and any recorded operation outcome shall commit or roll back in one PostgreSQL transaction. Conditional `UPDATE` is permitted instead of a separate `SELECT ... FOR UPDATE` where it enforces the same invariant.
5. **Deadlock Prevention**: All operations touching a reservation and one or more wallets shall use one documented lock order: lock the reservation row first when it already exists, then lock affected wallet rows in ascending UUID order. Reservation creation shall coordinate on the unique `errandId` and then lock the requester wallet. Do not acquire wallet locks and subsequently wait for a reservation lock.
6. **Reservation exclusivity**: `RESERVED` may transition exactly once to `PAID_OUT` or `REFUNDED`; the terminal outcomes are mutually exclusive. Recheck status while holding the reservation lock or via an atomic conditional transition.
7. **Balance conservation**: Except for the explicitly authorized registration allocation, each credit operation shall have a net zero change across the relevant available and reserved balance buckets. All signed bucket changes shall be recorded as part of one `operationId`.
8. **Registration allocation**: Each user shall receive the initial 100 credits at most once; wallet creation and the corresponding ledger entry shall be atomic. No generic wallet insert shall silently mint 100 credits.

---

## 2. Requirements Matrix

| ID | Description | Channel | Priority |
| :--- | :--- | :--- | :--- |
| **F4.1** | Allocate 100 credits to each user upon successful registration. | Async broker (registration event) | High |
| **F4.1.1** | Create a credit balance for each newly registered user. | Async broker | High |
| **F4.1.2** | Initialise the user's available credit balance to 100 credits. | Async broker | High |
| **F4.1.3** | Record the initial credit allocation in the user's credit transaction history within the same transaction as wallet initialization. | Async broker | High |
| **F4.1.4** | Ensure duplicate registration events cannot allocate initial credits more than once to the same user. | Async broker | High |
| **F4.2** | Reserve required credits from requester available balance upon errand request creation. | REST (`POST /reservations`) | High |
| **F4.2.1** | Reject reservation when requester has insufficient available credits. | REST | High |
| **F4.2.2** | Deduct reserved amount from requester available balance and transfer to reserved balance. | REST | High |
| **F4.2.3** | Transfer back reserved credits to available balance upon receiving errand creation failure notification. | Async broker (`errand.lifecycle.failed`) | High |
| **F4.3** | Transfer reserved credits to assigned courier upon receiving errand completion notification. | Async broker (`errand.lifecycle.completed`) | High |
| **F4.3.1** | Deduct errand reserved amount from requester reserved-credit balance. | Async broker | High |
| **F4.3.2** | Add errand reserved credit amount to assigned courier available-credit balance. | Async broker | High |
| **F4.3.3** | Ensure duplicate completion notifications have no additional financial effect; reject and flag completion when the reservation has already been refunded. | Async broker | High |
| **F4.4** | Release reserved credits upon receiving cancellation or expiry notification for an errand. | Async broker (`errand.lifecycle.cancelled` / `.expired`) | High |
| **F4.4.1** | Deduct reserved amount from requester reserved balance and add back to available balance. | Async broker | High |
| **F4.4.2** | Ensure duplicate cancellation/expiry notifications have no additional financial effect; reject and flag release when the reservation has already been paid out. | Async broker | High |
| **F4.5** | Adjust requester reserved credits when reward for an open errand is modified. | REST (`PATCH /reservations/:errand_id`) | Medium |
| **F4.5.1** | Reserve additional credits from available balance when reward increases. | REST | Medium |
| **F4.5.2** | Release difference to available balance when reward decreases. | REST | Medium |
| **F4.5.3** | Reject reward modification if requester has insufficient available credits for an increase. | REST | Medium |
| **F4.6** | Record comprehensive audit trail for every credit balance modification. | Database Ledger | Medium |
| **F4.6.1** | Record the reason for each balance change. | Database Ledger | Medium |
| **F4.6.2** | Record each signed credit delta together with the affected available or reserved balance bucket and a shared operation identifier for related entries. | Database Ledger | Medium |
| **F4.6.3** | Record the timestamp (date and time) of the balance change. | Database Ledger | Medium |
| **F4.6.4** | Associate unique `errandId` for all errand-related transactions. | Database Ledger | Medium |
| **F4.6.5** | Support non-errand balance updates with a `null` / optional `errandId`. | Database Ledger | Medium |
| **F4.7** | Expose authenticated read APIs for user credit account and history. | REST (`GET /accounts/me`) | Medium |
| **F4.7.1** | Return the user's available credit balance. | REST | Medium |
| **F4.7.2** | Return the user's reserved credit balance. | REST | Medium |
| **F4.7.3** | Return the paginated user credit transaction history. | REST | Medium |
| **F4.8** | Restrict credit-changing operations to authorized internal workflows; allow authenticated users to read only their own credit account and history. | Internal commands / REST | High |

---

## 3. Async Messaging Contract (RabbitMQ)

### 3.1 Broker-Agnostic Event Names
- `user.registered`
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
- A duplicate event whose reservation is already in the matching terminal state is acknowledged without mutating balances, provided its business details match the recorded operation.
- A conflicting terminal event (completion after refund, or cancellation/expiry after payout) shall not mutate balances; record the conflict for investigation and apply the configured poison-message policy rather than retrying indefinitely.
- The consumer shall verify `requesterId`, `amount`, and (for completion) `courierId` against the authorized reservation/settlement context; it shall not trust an event payload solely because it contains an existing `errandId`.
- Registration allocation shall be idempotent by `userId`, including when duplicate events have different `eventId` values.
- A malformed event is rejected and routed to the broker's dead-letter mechanism once that mechanism is recorded.
- Transient database errors must not be acknowledged before the consumer has confirmed the transaction outcome. After an ambiguous commit outcome, re-read the durable business state before retrying the financial effect.

### 3.4 RabbitMQ Details Still Open
<!-- AI-assisted clarification: D-042 answers broker choice only; topology and delivery policy remain owner/team decisions. -->
- Queue, exchange, routing key, consumer name, and dead-letter names.
- Retry policy and maximum delivery attempts.
- Serialization format and schema registry or schema-validation mechanism, if any.

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
    available_credits INTEGER NOT NULL DEFAULT 0,
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
    reverses_transaction_id UUID REFERENCES credit_transactions(id),
    operation_id UUID NOT NULL,
    balance_bucket TEXT NOT NULL CHECK (balance_bucket IN ('AVAILABLE', 'RESERVED')),
    amount INTEGER NOT NULL CHECK (amount <> 0),
    reason transaction_reason NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_transactions_user_created ON credit_transactions(user_id, created_at DESC);
CREATE INDEX idx_transactions_operation ON credit_transactions(operation_id);
CREATE INDEX idx_transactions_errand ON credit_transactions(errand_id);
CREATE UNIQUE INDEX uq_credit_transaction_operation_bucket
ON credit_transactions(operation_id, user_id, balance_bucket);
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

Credit-service follows the repository's service layering:
- `internal/credit`: domain models, repository interfaces, service methods, and sentinel errors.
- `internal/repository`: PostgreSQL adapter and transaction-bound persistence logic.
- `internal/httpapi`: HTTP DTOs, handlers, and Chi router.
- `internal/config`: environment loading.
- `internal/middleware`: identity propagation middleware after the team records that contract.
- `internal/gen`: generated OpenAPI code only; never hand-edited.
- `cmd/api`: composition root.
- `migrations`: reversible PostgreSQL migrations.

RabbitMQ adapter package naming is deferred until the topology and delivery
policy are recorded.

---

## 6. Transaction & Locking Rules

- Every balance mutation must run in one PostgreSQL transaction.
- Wallet mutations shall use a conditional SQL `UPDATE` (checking available balance where relevant) or explicit `SELECT ... FOR UPDATE` within the transaction. A plain read followed by an unprotected write is prohibited.
- For operations on an existing reservation, lock its row before wallet rows. Lock all affected wallets in ascending UUID order, including during completion transfers. Every mutation path shall follow the same order.
- Reservation creation shall coordinate on the unique `errand_id`, create or verify the reservation in the same transaction as wallet updates, and return the existing result for an identical retry. A changed requester or amount for an existing `errand_id` is a conflict.
- Reservation terminal transitions shall check the current status under the lock and atomically commit `RESERVED -> PAID_OUT` or `RESERVED -> REFUNDED` with all balance and ledger changes.
- Reward adjustments shall operate only on a `RESERVED` reservation and use the same reservation-first locking rule. The Order Service alone decides whether an errand is eligible for reward adjustment; the Credit Service enforces the authorized requested amount change, not the errand lifecycle rule.
- Ledger entries shall record both sides of reservations, releases, and transfers as signed bucket deltas sharing an `operation_id`. Wallet balances and ledger entries shall be committed together.
- Wallet `version` shall increment on mutation. The field does not by itself provide optimistic concurrency unless a version predicate is explicitly used.
- No external network call shall occur while holding database locks. Keep transactions short and use bounded retries for deadlocks/serialization failures.
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
- `GET /accounts/me` and `/accounts/me/transactions` shall derive the subject from the verified identity; clients cannot select another user's account.
- `POST /reservations` and `PATCH /reservations/{errandId}` are credit-changing workflow commands and shall require an authorized Order Service caller or another explicitly trusted internal workflow. A valid end-user JWT alone is insufficient authorization.
- The Credit Service shall use authenticated caller context, not a client-supplied requester ID, to establish the requester identity and shall verify the authorized workflow context before changing credits.
- Completion/release event consumers shall accept messages only from trusted producers and validate the event against the reservation and permitted operation. Broker access controls and producer identity must be configured before deployment.

### 7.2 Error Envelope and Status Mapping
- Error response body: `{"error": "<message>"}`.
- `400 Bad Request`: syntax errors or invalid input, such as `amount <= 0`.
- `401 Unauthorized`: missing or invalid token.
- `404 Not Found`: entity not found.
- `409 Conflict`: conflicting reuse of an existing `errandId` (different requester/amount), or modifying a non-`RESERVED` hold. Identical reservation retries return the original reservation outcome without another deduction.
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
- `TransactionItem`: `id` UUID, `userId` UUID, nullable `errandId` UUID, `operationId` UUID, `balanceBucket` (`AVAILABLE` or `RESERVED`), `amount` integer, `reason` `TransactionReason`, `createdAt` date-time.
- `Pagination`: `limit` integer, `offset` integer, `total` integer.
- `TransactionListResponse`: `items` array of `TransactionItem`, `pagination` `Pagination`.

### 7.5 Endpoint Responses
- `POST /api/v1/credits/reservations`: `201 ReservationResponse` for new reservations; `200 ReservationResponse` for identical retries; errors `400`, `401`, `403`, `409`, `422`.
- `PATCH /api/v1/credits/reservations/{errandId}`: `200 ReservationResponse`; errors `400`, `401`, `403`, `404`, `409`, `422`.
- `GET /api/v1/credits/accounts/me`: `200 AccountResponse`; errors `401`, `404`.
- `GET /api/v1/credits/accounts/me/transactions`: `200 TransactionListResponse`; errors `400`, `401`.

---

## 8. Deferred Cross-Service Recovery and Operational Checks

- **Deferred by owner**: recovery for ambiguous order-creation outcomes after a successful reservation (for example, Order Service crash before recording the result). F4.2.3 remains the current failure-notification compensation path; it does **not** guarantee recovery if that notification is never published. This remains an explicit integration risk, not an implemented guarantee.
- **Order Service ownership**: the Order Service decides when an errand's reward may be changed. The Credit Service only verifies the reservation is still `RESERVED` and applies an authorized adjustment atomically.
- **Operational checks to implement**: detect long-lived reservations for investigation (without automatically releasing them), repeated consumer failures, conflicting terminal events, and discrepancies between wallet balances and ledger bucket totals.
- **Concurrency acceptance checks**: simultaneous reservations cannot overspend; duplicate registration allocates once; duplicate completion pays once; competing completion/release yields only one terminal financial outcome; concurrent transfers to one courier produce the correct final balance.
- **Performance measurement**: capture p95/p99 transaction latency, lock wait duration, deadlock count, and connection-pool saturation under a documented workload before setting numerical thresholds.

---

## 9. Implementation Readiness Checklist

Ready now:
- Database schema migration and down migration from Section 4, including ledger bucket and operation identifiers.
- Domain models and constants from Section 1.
- Repository transaction logic from Section 6, including consistent reservation/wallet lock ordering and idempotent business outcomes.
- Service methods for registration allocation, reservation, reward adjustment, account read, and history read. Registration allocation must be callable from an async consumer adapter rather than an HTTP handler.

Blocked until further owner/team records:
- Broker adapter implementation.
- Broker-specific topology and retry/dead-letter behavior, including conflicting terminal-event handling.
- Event consumer code that depends on exact broker library and delivery semantics.

- Internal caller authentication/authorization contract for credit-changing commands must be agreed with the Order Service before exposing those operations.
