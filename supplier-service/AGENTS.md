# Supplier Service

Authority on campus suppliers — the stores, facilities and pickup locations an
errand can point at (mandatory requirement M3). It owns the supplier record, the
rules for changing one, and its own Postgres database. It deliberately knows
nothing about errands, orders, matching, users or credits: a supplier has no
idea it has ever been ordered from, and nothing in this folder should teach it.

This is the only implemented service in the repo, so its layering is the
reference the other four copy (root §6 points here by name). Changing the
*shape* of that layering is a repo-wide decision, not a local one.

**Status: implemented, but not on this branch.** `supplier-service/` here holds
empty `Dockerfile` and `README.md` placeholders; the implementation is on
`origin/feat/supplier-service` (open PR #1, "Supplier Service + homepage
frontend"). Read it before concluding something is missing:

```bash
git ls-tree -r --name-only origin/feat/supplier-service:supplier-service
```

## Owner

TBD — one developer owns this folder (root §3). Requirements, table design and
endpoint shape are theirs to decide, and land in `ai/decisions.md` before an
agent builds to them.

## Boundaries

- **`order-service` (does not exist yet).** The pressure here is real: knowing
  whether a supplier is referenced by a live errand needs order data. Do not add
  an orders table, a foreign key, or a second connection string to reach for it.
  When `order-service` exists, ask it over its API or consume its events. Until
  then the interim soft delete below is the answer.
- **`user-service`.** Admin-only endpoints need a role, not a user. Do not query
  the users table and do not fetch a user record to inspect it — the role
  arrives with the request (see Gotchas).
- **`credit-service`.** Prices, wallets and payouts are not supplier fields. A
  supplier is a place, not a price list.
- **`frontend`.** It will eventually want a supplier merged with data this
  service does not hold. Compose that at the caller; do not grow a field here
  that only another service can fill.
- **Others reading suppliers.** Point them at this service's HTTP API. Never
  share `SUPPLIER_DB_URL`, and never factor the `Supplier` struct into a package
  they import — a duplicated struct on their side is the correct outcome
  (root §4.2).
- **`../data/csv/` and `../data/images/`** are repo-level shared assets, not
  owned here. This service only reads the CSV. Editing either is a shared-file
  change: flag it (root §3).

## Layout

Module path `foc/supplier-service`. The per-file detail the other services
mirror:

```
cmd/api/             wiring only — config, pool, migrate, seed, router, listen
internal/supplier/   the domain: model.go, service.go (business rules),
                     repository.go (the Repository *interface*), errors.go
                     (sentinel errors the transport layer maps to status codes),
                     service_test.go
internal/repository/ Postgres adapter implementing supplier.Repository, via pgx
internal/httpapi/    router.go, handlers.go, dto.go — JSON shapes live in dto.go
internal/middleware/ request guards (currently the admin-role check)
internal/config/     Load() reads env once into a Config struct, then passed down
seed/                first-boot CSV load; depends on the domain, not vice versa
migrations/          golang-migrate .up/.down pairs, applied at startup from
                     cmd/api/main.go
```

## Local development

Port **8082** (root §3). Env vars `PORT`, `SUPPLIER_DB_URL`, `SEED_CSV_PATH`,
with placeholders in the root `.env.example` and this folder's own.

```bash
go run ./cmd/api     # from supplier-service/; migrates then seeds on startup
go test ./...        # unit tests: service layer + CSV parser, no Docker needed
docker compose up supplier-service supplier-db    # from the repo root
```

## Gotchas

- **The seed CSV is Windows-1252, not UTF-8.** `seed/seed.go` decodes it through
  `charmap.Windows1252` because the building name "Prince George's Park" carries
  a 0x92 curly apostrophe; read it as UTF-8 and you get mojibake in the database.
  It also has a quoted field containing a comma and CRLF line endings — use the
  CSV reader, never `strings.Split`.
- **The Dockerfile's build context is the repository root**, not this folder
  (`compose.yaml` sets `context: .` with `dockerfile: supplier-service/Dockerfile`),
  because the image needs that shared CSV, so paths inside it start with
  `supplier-service/`. Unusual — do not copy it as the pattern elsewhere.
- **DELETE is a soft delete** — `Service.Delete` calls `repo.SoftDelete`, which
  sets `deleted_at` and flips `is_available` to false rather than removing the
  row. Interim, because the rule it should enforce depends on `order-service`.
  Revisit when that service lands.
- **Admin auth trusts an `X-User-Role: ADMIN` header** — interim, and not
  secure. Swapping `HeaderRoleExtractor` for the real mechanism is the only
  change needed here once the team decides it.
- **There is no `api/openapi.yaml` yet**, so the DTOs in `internal/httpapi` are
  hand-written and there is no `internal/gen/`. Root §8 is still the target;
  writing that spec is an interface decision for the owner, not for you.
