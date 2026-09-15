# Order Service

The authority on the errand lifecycle (mandatory requirement M4): an order's
existence, the state it is in, and which user IDs are attached to it as
requester and courier. It orchestrates the core product workflow and is the most
state-heavy, concurrency-sensitive service in the repo. It deliberately knows
nothing about credit balances, supplier records or user profiles — it stores IDs
belonging to `credit-service`, `supplier-service` and `user-service`, and asks
those services when it needs more than an ID.

## Owner

TBD — one developer owns this folder (root §3). Everything under "Owner's
decisions" is theirs; an agent implements one recorded in `ai/decisions.md`
and stops.

## Boundaries

- **Credits are not this service's state.** Reserve and settle through
  `credit-service`'s API. Never read or write its tables, never recompute a
  balance here, never persist "the requester could afford it" on the order. For
  an order's credit outcome, ask `credit-service` or consume its event.
- **Pickup locations are validated, not owned.** A supplier ID is checked
  against `supplier-service`. Treat that service as something that can be slow
  or down: bounded timeout, retry with a deadline, and a defined behaviour when
  it does not answer — assuming it is up is exactly the temporal coupling root
  §5 calls out. Keep the ID; do not copy supplier fields into the order (a whole
  `Supplier` where an ID would do is stamp coupling).
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
implement the one they recorded in `ai/decisions.md`. Two constraints from the
root file still apply whatever they choose: no boolean flag parameter selecting
which transition to run (§5, control coupling), and no status writes in
handlers, because business rules live in the service layer (§6).

The corner cases the project document names — only one courier is ever assigned,
a requester cannot accept their own errand — are given with "such as", so the
list is not exhaustive. Whichever structure the owner picks has to keep them
testable against a fake repository, with no database.

## Owner's decisions

Graded D3 architecture decisions, off-limits to an agent (root §1). Do not
propose answers; implement the decision the owner recorded in
`ai/decisions.md`:

- the set of states, and which transitions are legal;
- how concurrent acceptance of the same errand is made safe;
- whether credit reservation happens before, or atomically with, persisting the
  order;
- what happens to an errand that stalls after acceptance;
- which lifecycle events are published asynchronously, and what they carry.

## Local development

**Status: not implemented on any branch** — the folder holds only these
instruction files and empty `Dockerfile` / `README.md` stubs. The first PR
scaffolds `go.mod`, the packages above, an empty `migrations/` and the
`/health` endpoint that `supplier-service` already exposes. Adding
`order-service` and its database to the root `compose.yaml` and `.env.example`
touches shared files, so flag it in the PR.

Port **8083**, database URL `ORDER_DB_URL`, plus one `<SERVICE>_BASE_URL` per
service it calls (root §3). From `order-service/`: `go run ./cmd/api`,
`go test ./...`, and `go test -tags=integration ./...` (needs Docker).

## Gotchas

- `order` is a reserved word in SQL. However the owner names tables and columns,
  quote or avoid the bare word rather than discovering it at migration time.
- Expiry of never-accepted errands needs a clock. Inject it instead of calling
  `time.Now()` in the domain, so expiry is testable without waiting (root §7:
  never sleep in a test).
- How identity and role travel between services is undecided; the interim
  `X-User-Role` header in `supplier-service` exists only so it has something to
  test against. Requester-vs-courier authorisation here depends on the real
  mechanism, so build nothing load-bearing on that header.
