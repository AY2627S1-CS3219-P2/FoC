# Credit Service

Credit Service manages the financial credit ledger for Friend on Campus (FoC). It guarantees strict credit conservation, idempotent double-entry balance movements, append-only auditability, and automated ledger reconciliation.

## Architecture & Invariants

1. **Credit Conservation & Finite Supply**:
   - Credits originate exclusively from initial user registration allocations (100 credits per user).
   - Account balances are tracked in two non-negative 64-bit integer buckets: `available_credits` and `reserved_credits`.
   - Balances are strictly bounded within `[0, 1,000,000,000]` credits.

2. **Append-Only Immutable Ledger**:
   - The `credit_transactions` table records all credit movements with debit and credit deltas.
   - PostgreSQL triggers enforce immutability by rejecting all `UPDATE` and `DELETE` operations on ledger rows.
   - Projections (`credit_accounts`) reflect the cumulative sum of committed ledger deltas.

3. **Concurrency & Deadlock Prevention**:
   - Multi-account operations (such as completion payouts involving requester and courier) acquire row-level locks in deterministic ascending UUID order.
   - Transactions execute with strict statements and lock timeouts (`lock_timeout = 1s`, `statement_timeout = 5s`) and bounded exponential-backoff retries.

4. **Integrity Auditing & Reconciliation**:
   - A dedicated `ReconciliationWorker` periodically audits account balances against the transactions ledger.
   - If an account projection diverges from the ledger, its status is set to `FLAGGED` to prevent further financial mutations while leaving unaffected accounts fully operational.

---

## HTTP API Surface

### Public End-User Routes (Authenticated via Bearer JWT)
- `GET /api/v1/credits/accounts/me`: Retrieve current user's available and reserved balance aggregate.
- `GET /api/v1/credits/accounts/me/transactions`: Retrieve cursor-paginated transaction history grouped by operation ID.

### Private Inter-Service Routes (Private Network Only)
- `POST /api/v1/credits/reservations`: Atomically reserve requester credits for an errand. Accepts `requesterId` in body or extracts it from caller's JWT Bearer token context if omitted. (Alias: `/internal/v1/credit-reservations`).
- `PATCH /api/v1/credits/reservations/{errandId}`: Atomically adjust an errand's reserved reward to target `newReward`. Reads operation ID from `X-Request-Id` header (no caller CAS needed). (Alias: `/internal/v1/credit-reservations/{errandId}`).

### Health Endpoints
- `GET /api/v1/health`: Dependency health check verifying PostgreSQL ping, schema migration version, and RabbitMQ supervisor status.

---

## Asynchronous Messaging (RabbitMQ)

The service consumes events on durable queues bound to the `foc.events` topic exchange:

1. **User Registration (`credit.registration` queue)**:
   - Listens for `user.registered` events.
   - Idempotently creates the user account and allocates 100 credits.

2. **Errand Lifecycle (`credit.errand.lifecycle` queue)**:
   - `errand.completed`: Settles completion payout from requester hold to courier available balance (`PAID_OUT`).
   - `errand.cancelled`, `errand.expired`, `errand.failed`: Restores requester held credits back to available (`REFUNDED`).
   - Failed deliveries retry up to 5 times with jittered backoff before being dead-lettered to `foc.dlx`.

---

## Background Workers

- **Pending Payout Worker**: Claims and retries payouts that were delayed due to missing courier accounts or lock contention (`FOR UPDATE SKIP LOCKED`).
- **Pending Refund Worker**: Claims and retries delayed refunds until committed.
- **Reconciliation Worker**: Acquires PostgreSQL session advisory lock (`0x435245445F524543`), performs repeatable-read batch snapshot audits, and flags discrepancies.

---

## Local Development & Testing

### Running with Docker Compose
From the repository root:
```bash
docker compose up credit-service
```

### Running Directly on Host
1. Set up the local environment:
   ```bash
   cp .env.example .env
   ```
2. Start dependencies (PostgreSQL and RabbitMQ):
   ```bash
   docker compose up credit-db rabbitmq -d
   ```
3. Run the service:
   ```bash
   go run ./cmd/api
   ```

### Running Tests
Execute the full unit test suite:
```bash
go test -count=1 ./...
```

Execute the deterministic property-based state machine test (200 seeds of 100 steps each):
```bash
go test -v -run TestCreditStateMachine ./internal/credit/...
```
