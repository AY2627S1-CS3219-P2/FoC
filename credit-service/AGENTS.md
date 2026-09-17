# Credit Service

The ledger of the platform's closed credit economy (mandatory requirement
**M5**). This service is the sole authority on how many credits an account has,
how many are committed to in-flight errands, and the record of every change to
both. It deliberately knows nothing about errands beyond the identifiers and
amounts it is told: not what was ordered, not where from, not whether a courier
behaved, not who a user is beyond an account ID. It never decides that an errand
completed — it records the consequence when told.

## Owner

**TBD** — one developer owns this folder (root §3). The design decisions listed
below are theirs to make; an agent implements them once they are written down
(root §1).

## Boundaries

**Hard invariant: credits are conserved.** This is from the project brief, not
a design choice — M5 and the project context state that credits are allocated at
registration and are otherwise only earned by completing errands for others, and
that the economy is closed. So the system total changes only when a new account
receives its initial allocation (the size of that allocation is the team's to
choose).
Reservation, transfer on completion, and release on cancellation or expiry all
*move* credits; none of them mints or destroys any. Any code path that can
change the system total outside that one case is a bug, and this is the single
most valuable property for tests to assert — sum the ledger before and after a
generated sequence of operations and require it to be unchanged.

- **Closed means closed.** No purchase, top-up, withdrawal, payout,
  refund-to-money or exchange-rate operation exists, and this service must
  expose none. Asked for one, stop: it contradicts the project brief.
- **No one else writes a balance, and no one else keeps a copy of one.**
  `order-service`, `user-service` and `frontend` read balances through this
  service's HTTP API. A balance cached elsewhere is a second source of truth
  and will drift.
- **React to order lifecycle facts; do not decide them.** `order-service` owns
  whether an errand was created, completed, cancelled or expired. This service
  is told, and moves credits accordingly. Do not call back into
  `order-service` to ask whether an errand "should" settle — that inverts the
  ownership and couples the two services' rules. A lifecycle fact missing
  something needed to settle it is a contract gap for the two owners to close,
  not a lookup to add here.
- **This service learns that an account exists; it does not create or validate
  accounts.** The project brief requires the initial allocation to happen once
  per account, so hearing about the same registration twice must not allocate
  twice. *Where* that guard lives, and how it works, is the owner's decision —
  do not assume an upstream service will deduplicate for you, and do not pick
  the mechanism yourself.
- **Keep foreign models out.** Store the identifiers you are given, not copies
  of user profiles or order rows (stamp coupling, root §5).

**Not an agent's to decide** — the owner does, and D3 grades these: how balances
and their committed portion are modelled; how atomicity is achieved for credit
operations; how duplicate or retried lifecycle events are made idempotent; and
whether reservation is synchronous while settlement is asynchronous. The API
surface and the tables are interface and schema decisions under root §1 — hand
them back to a human.

## Layout

Mirror `supplier-service` package for package (root §6 and that folder's
`AGENTS.md`); module path `foc/credit-service`; the domain package is
`internal/credit`.

The one local note: `internal/middleware/` stays empty here until the team
settles how identity is propagated between services. How the ledger itself is
shaped is a schema decision listed above — not yours.

## Local development

**Status: not implemented on any branch.** The folder holds empty `Dockerfile`
and `README.md` placeholders — no `go.mod`, no `cmd/api`. The first PR here
scaffolds it; say so rather than pretending a target already runs.

Port **8084**, database URL `CREDIT_DB_URL` (root §3), with placeholders in the
root `.env.example` and this service's own once it has one.

Once scaffolded: `go run ./cmd/api`, `go test ./...`,
`go test -tags=integration ./...`, and
`docker compose up credit-service credit-db` from the repo root.
`compose.yaml` is shared — flag it when you touch it (root §3).

## Gotchas

- **Flag floating-point credit amounts.** If the recorded model or schema
  represents credits as `float64`, say so before implementing it rather than
  going ahead — rounding drift in a ledger is silent and unrecoverable.
- **Check-then-write is a race.** Reading an available balance and then writing
  a reservation in a second statement lets two concurrent errands both pass the
  check and overdraw. The fix belongs at the repository/transaction boundary;
  the mechanism is the owner's call.
- Unit tests against an in-memory fake repository (root §7) cannot demonstrate
  atomicity or isolation — concurrency claims need the integration tier, a real
  Postgres, and several goroutines at once.
