# Order Service

The authority on the errand lifecycle (mandatory requirement M4): an order's
existence, the state it is in, and which user IDs are attached to it as
requester and courier. It orchestrates the core product workflow and is the most
state-heavy, concurrency-sensitive service in the repo. It deliberately knows
nothing about credit balances, supplier records or user profiles — it stores IDs
belonging to `credit-service`, `supplier-service` and `user-service`, and asks
those services when it needs more than an ID.

## Owner

James Teo (D-044). Everything under "Owner's decisions" is theirs; an agent
implements one the owner has recorded (root §1) and stops.

## Boundaries

- **Credits are not this service's state.** Reserve a reward and hold more
  for a raise through `credit-service`'s API before saving; release a lowered
  reward through that API right after saving, with the saved
  `errand.reward_set` as the guarantee if that call fails (D-102); settle only
  through the lifecycle events this service
  publishes via its outbox, whose `amount` is the errand's reward at that
  transition and is what settles (D-059). Never read or write its tables, never recompute a balance here, never persist "the
  requester could afford it" on the order. For an order's credit outcome, ask
  `credit-service` or consume its event.
- **Pickup locations are validated, not owned.** A supplier ID is checked
  against `supplier-service`. Treat that service as something that can be slow
  or down: bounded timeout, retry with a deadline, and a defined behaviour when
  it does not answer — assuming it is up is exactly the temporal
  coupling root §5 calls out. Keep the ID, plus the supplier's name and location
  copied at create and amend so a later deactivation or deletion cannot erase
  them from an errand. Copy nothing else: a whole `Supplier` where those
  fields would do is stamp coupling.
- **User IDs, not user records.** Names, contact details and ratings belong to
  `user-service`; resolve them at the edge, never denormalised into an order.
- If a change here looks like it needs an edit inside `credit-service/`,
  `supplier-service/` or `user-service/`, stop and say so: that is a contract
  change and belongs to that folder's owner.

## Layout

Mirror `supplier-service` package for package (root §6 and that folder's
`AGENTS.md`); module path `foc/order-service`; the domain package is
`internal/order`.

**How state transitions are structured is the owner's decision** (root §1) —
implement the one they recorded. Two constraints from the
root file still apply whatever they choose: no boolean flag parameter selecting
which transition to run (§5, control coupling), and no status writes in
handlers, because business rules live in the service layer (§6).

The corner cases the project document names — only one courier is ever assigned,
a requester cannot accept their own errand — are given with "such as", so the
list is not exhaustive. Whichever structure the owner picks has to keep them
testable against a fake repository, with no database.

## Owner's decisions

Graded D3 architecture decisions, off-limits to an agent (root §1). Do not
propose answers; implement the decision the owner recorded. The rows in
`ai/decisions.md` that record them:

- the set of states, and which transitions are legal — D-045, D-046, D-060;
- how many `OPEN` errands stop a requester posting another — D-058;
- how concurrent acceptance of the same errand is made safe — D-046;
- whether credit reservation happens before, or atomically with, persisting the
  order — D-047;
- what happens to an errand that stalls after acceptance — D-050;
- which lifecycle events are published asynchronously, and what they carry —
  D-048, D-051, D-057, D-059, D-102 (D-042 binds four of the keys, and
  `rabbitmq/definitions.json` binds `errand.reward_set` too; D-102 is the
  cross-service row, and credit-service's half of it is pending with its owner,
  #68).

The order requirements themselves are in `docs/requirements.md`.

## Scheduled Errands

[//]: # AI Assistance Disclosure:
[//]: # Tool: Codex (model: GPT-6), date: 2026-10-28
[//]: # Scope: Transcribed the owner's approved scheduled-errand behavior below.
[//]: # Author review: ZI YANG - decided on implementation before asking Codex to note it down

Support planned errands such as pre-ordered coffee runs, reusing the existing
expiry scheduler. A scheduled errand is in `SCHEDULED` until its scheduled time,
then becomes `OPEN`. Times use UTC, consistent with the order service's existing
clock and RFC 3339 API timestamps. `POST /` includes a `scheduled` field:
the field is optional, and an omitted value or `""` means a normal errand; a
timestamp schedules it. Errand responses and listings include `scheduled`,
using `""` for normal errands.

- A scheduled time must be in the future and may be selected starting at 00:00
  UTC on the calendar day before its scheduled date. For example, an errand
  scheduled for 2026-10-10 at 14:00 UTC may be scheduled starting 2026-10-09 at
  00:00 UTC. The database column is `SCHEDULED_TIME`, nullable, and its value
  must be at or after `created_at`.
- While `SCHEDULED`, the requester may amend or cancel the errand. It appears in
  the requester's listings, but not in couriers' listings. The requester may
  change the scheduled time while the errand is `SCHEDULED`; a replacement time
  follows the scheduling rules above. Setting `scheduled` to `""` removes the
  schedule and makes the errand `OPEN` immediately, subject to the five-`OPEN`
  limit.
- When a requester changes the scheduled time, record the change in
  `history.changes` as field `scheduled`, with RFC 3339 UTC values; use `""`
  as the `to` value when the schedule is removed.
- Record the system transition from `SCHEDULED` to `OPEN` with history action
  `OPEN`.
- Reserve credits when the scheduled errand is created. Do not recheck credits
  when it transitions from `SCHEDULED` to `OPEN`.
- `SCHEDULED` errands do not count toward the requester's limit of five `OPEN`
  errands. If the limit prevents an errand becoming `OPEN`, whether at its
  scheduled time or when its schedule is removed, cancel it through the normal
  cancellation flow. Notification delivery for this cancellation is deferred
  until the notification service is created.
- Start the expiry period when the errand becomes `OPEN`. If it remains
  unaccepted until that period elapses, expire it and clean it up through the
  normal expiry flow.

## Local development

**Status:** runs in Compose behind the gateway, with supplier-service,
credit-service and RabbitMQ; the whole-stack suite in `e2e/` covers its
lifecycle.

Port **8083**, database URL `ORDER_DB_URL`, `SUPPLIER_BASE_URL`,
`CREDIT_BASE_URL`, `RABBITMQ_URL`, `JWKS_URL`, `REDIS_URL`, and T, K and C as
`ORDER_EXPIRY_PERIOD`, `ORDER_MAX_ACTIVE_ASSIGNMENTS` and
`ORDER_CONFIRMATION_WINDOW`. From `order-service/`: `go run ./cmd/api`,
`go test ./...`, and
`go test -tags=integration ./...` (needs Docker). After changing
`api/openapi.yaml`, run `go generate ./internal/gen` and commit the result.
The whole-stack suite lives in the root `e2e/` module.

## Gotchas

- `order` is a reserved word in SQL. However the owner names tables and columns,
  quote or avoid the bare word rather than discovering it at migration time.
- Expiry of never-accepted errands needs a clock. Inject it instead of calling
  `time.Now()` in the domain, so expiry is testable without waiting (root §7:
  never sleep in a test).
- Identity comes from the caller's access token, verified in one middleware
  against user-service's keys and its revocation keys in the shared Redis;
  nothing else reads it, and `X-User-Id` and `X-User-Role` are ignored. The per-errand
  relationship (requester, assigned courier, ADMIN or none) is computed from
  the token's `sub` and `role` on every action. Calls to `credit-service`
  forward the token the request arrived with; only the requester reserves or
  amends, so it is always the requester's.
