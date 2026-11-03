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
- `PATCH /api/v1/credits/reservations/{errandId}`: Set the hold to `newReward` using required positive `currVersion` from the locked order; Credit Service records `currVersion + 1`. An increase secures credits before Order Service commits. A decrease releases credits after Order Service commits. **Retry idempotency is guaranteed only when the caller supplies a valid UUID in `X-Request-Id` and reuses that same ID and version for retries of the same logical adjustment.** If omitted, Credit Service generates a new ID for each call. (Alias: `/internal/v1/credit-reservations/{errandId}`).

### Health Endpoints
- `GET /api/v1/health`: Dependency health check verifying PostgreSQL ping, schema migration version, and RabbitMQ supervisor status.

---

## Asynchronous Messaging (RabbitMQ)

The service consumes events on durable queues bound to the `foc.events` topic exchange:

1. **User Registration (`credit.registration` queue)**:
   - Listens for `user.registered` events.
   - Idempotently creates the user account and allocates 100 credits.

2. **Errand Lifecycle (`credit.errand.lifecycle` queue)**:
   - `errand.completed`: Pays the event's positive amount from the requester hold to the courier and returns any surplus hold to the requester in the same transaction. An amount above the hold is rejected to the lifecycle DLQ without changing credits.
   - `errand.reward_set`: Records the committed order version and releases excess held credits. It never increases a hold; older and duplicate events have no financial effect.
   - `errand.cancelled`, `errand.expired`, `errand.failed`: Restores requester held credits back to available (`REFUNDED`).
   - Failed deliveries retry up to 5 times with jittered backoff before being dead-lettered to `foc.dlx`.

---

## Background Workers

- **Pending Payout Worker**: Claims and retries payouts delayed by a missing courier account, flagged account, or courier balance limit (`FOR UPDATE SKIP LOCKED`). It retains the completion amount and returns any surplus hold to the requester.
- **Pending Refund Worker**: Claims and retries delayed refunds until committed.
- **Reconciliation Worker**: Acquires PostgreSQL session advisory lock (`0x435245445F524543`), performs repeatable-read batch snapshot audits, and flags discrepancies.

## Reward Hold Recovery & DLQ Replay Procedure

An increase can leave an excess hold if Order Service fails to commit after Credit Service secures the extra credits. A committed decrease can also leave an excess hold if its release call or `errand.reward_set` delivery fails. The excess remains reserved; completion pays exactly the event amount and returns the rest, while cancellation or expiry returns the full hold. A later successful reward change can also reset the hold. Do not release an excess hold solely because time has passed: Order Service's committed state must be checked first.

Monitor `credit.errand.lifecycle.dlq` and the lifecycle consumer's structured errors. Credit Service logs a structured warning on startup if any Dead Letter Queue contains messages. For an `errand.reward_set` in the DLQ, record its `eventId`, `errandId`, amount, and version; compare them with the committed order and the reservation before an authorized replay through the documented lifecycle replay procedure. Replay the original event identity so processing remains idempotent. Escalate a completion amount above the hold for investigation; replay it only after the producer and financial state are reconciled. Credit Service does not repair balances automatically.

### DLQ Replay Procedure
When replaying dead-lettered events after resolving the root cause:
1. Fetch the dead-lettered event payload and note its original routing key (`errand.completed`, `errand.reward_set`, `errand.cancelled`, etc.).
2. Republish the message body to exchange `foc.events` with its **original routing key** and header `content_type: application/json`.
   > **WARNING**: Do **not** move/shovel messages directly back into `credit.errand.lifecycle`. Moving a message directly delivers it with the queue's name as its routing key, causing `validateTransport` to reject it right back to the DLQ. Republishing to `foc.events` routes it correctly.
3. Remove/ack the message from the DLQ only after successful republishing.

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
