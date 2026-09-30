<!--
AI Assistance Disclosure:
Tool: Claude Code (model: Sonnet 5), date: 2026-09-22
Scope: Rewritten (>half the file changed): Status paragraph corrected
  (PR #1 merged; this branch now holds the REST->gRPC change), Layout and
  Gotchas updated for internal/grpcapi, internal/gen, proto/, and the
  removal of internal/httpapi.
Author review: PENDING — <reviewer to complete>
-->

# Supplier Service

Authority on campus suppliers — the stores, facilities and pickup locations an
errand can point at (mandatory requirement M3). It owns the supplier record, the
rules for changing one, and its own Postgres database. It deliberately knows
nothing about errands, orders, matching, users or credits: a supplier has no
idea it has ever been ordered from, and nothing in this folder should teach it.

This is the only implemented service in the repo, so its layering is the
reference the other four copy (root §6 points here by name). Changing the
*shape* of that layering is a repo-wide decision, not a local one.

**Status:** implemented and merged to `main` (PR #1). **Interface is gRPC
only as of 2026-09-22** — the former REST API (`internal/httpapi`) was
removed; see `ai/decisions.md` and the Gotchas below for what that breaks
elsewhere until the gateway and frontend catch up. This work is on
`feat/supplier-service-grpc`, branched from `feat/supplier-service`, and
not yet merged — read that branch, not `main`, to see it:

```bash
git ls-tree -r --name-only feat/supplier-service-grpc:supplier-service
```

## Owner

GCheeYang (D-034). Requirements, table design and RPC shape are theirs to
decide, and must be recorded before an agent builds to them (root §1).

The gRPC-only interface and the contract in `proto/foc/supplier/v1/supplier.proto`
are recorded as D-035, confirmed by the owner. D-034 and D-035 are being added
to `ai/decisions.md` by their own PR (branch `docs/supplier-owner-and-contract`),
separate from this code, so they may not be on `main` yet.

Also decided by the owner: `is_available` stays a plain `bool` (D-035), and the
browser-facing JSON keeps snake_case field names and `{"error": "<message>"}`
errors (D-036). The names are pinned in the `.proto` with `json_name`; the
`{"error": ...}` body is built by the gateway from supplier's gRPC status and
message, not by this service.

The public REST URLs are decided too (D-037): all five RPCs are reachable from
the browser under `/api/suppliers`, and create, update and delete are
admin-only. They are `google.api.http` options in the `.proto`; this service
serves no REST itself. `search` is queried as `q` through `json_name`.

Identity is decided for this service's side (D-038, which applies D-022 and
D-030 to gRPC): the gateway verifies the user's token, discards any identity
the client sent and injects the verified role; this service reads it from the
`x-user-role` metadata key and trusts it, and never sees the token. The role
originates in user-service (`users.account_role`, the token's `role` claim).
Create, update and delete require exactly `ADMIN`; a missing or repeated value
counts as no role. Reads need no role here: the gateway already rejects
unauthenticated browser calls, and internal callers are services, not users.

**That trust is not sound yet** — D-022's two conditions both fail today:
the gateway does not yet forward identity to gRPC services or strip
client-supplied metadata (Nigeltzy, D-026), and `compose.yaml` still publishes
`:8082`, so anything can reach this service directly and claim `ADMIN`.
Removing the port is a shared-file change tied by its own comment to the
gateway change that lands the proxy, so it is flagged, not made, here.

Code is generated with `buf` (D-039).

Still open, and the owner's to decide and record before an agent builds to
them: the metadata key names and service-to-service identity in the Open list in
`ai/decisions.md`.

`buf lint` is clean (D-040): the `.proto` lives at `proto/foc/supplier/v1/`, each RPC
has its own response message, and `Get`/`Create`/`Update` wrap the `Supplier` in a
`supplier` field. Their `google.api.http` rules use `response_body: "supplier"`, so the
browser-facing JSON is still the flat object the frontend reads. Nothing enforces
lint yet (no CI).

## Boundaries

- **`order-service` (does not exist yet).** The pressure here is real: knowing
  whether a supplier is referenced by a live errand needs order data. Do not add
  an orders table, a foreign key, or a second connection string to reach for it.
  When `order-service` exists, ask it over its API or consume its events. Until
  then the interim soft delete below is the answer.
- **`user-service`.** Admin-only RPCs need a role, not a user. Do not query
  the users table and do not fetch a user record to inspect it — the role
  arrives with the call (see Gotchas).
- **`credit-service`.** Prices, wallets and payouts are not supplier fields. A
  supplier is a place, not a price list.
- **`frontend`.** It will eventually want a supplier merged with data this
  service does not hold. Compose that at the caller; do not grow a field here
  that only another service can fill.
- **Others reading suppliers.** Point them at `proto/foc/supplier/v1/supplier.proto`
  and generate a client from it — never a hand-rolled connection (root §4.3).
  Never share `SUPPLIER_DB_URL`, and never factor the `Supplier` struct into a
  package they import — a duplicated struct on their side is the correct
  outcome (root §4.2).
- **`../data/csv/` and `../data/images/`** are repo-level shared assets, not
  owned here. This service only reads the CSV. Editing either is a shared-file
  change: flag it (root §3).

## Layout

Module path `foc/supplier-service`. The per-file detail the other services
mirror:

```
cmd/api/             wiring only — config, pool, migrate, seed, gRPC server, listen
internal/supplier/   the domain: model.go, service.go (business rules),
                     repository.go (the Repository *interface*), errors.go
                     (sentinel errors the transport layer maps to status codes),
                     service_test.go
internal/repository/ Postgres adapter implementing supplier.Repository, via pgx
internal/grpcapi/    server.go (RPC handlers), convert.go (proto<->domain),
                     errors.go (domain error -> grpc/codes), router.go
                     (grpc.Server wiring: admin interceptor, health, reflection),
                     server_test.go (in-process, via bufconn, no Docker)
internal/gen/        generated from proto/ — never hand-edit (root §8)
                     (imports google.golang.org/genproto/googleapis/api for the
                     google.api.http options)
internal/middleware/ request guards (currently the admin-role check, via
                     gRPC metadata — see Gotchas), auth_test.go
internal/config/     Load() reads env once into a Config struct, then passed down
proto/foc/supplier/v1/ supplier.proto — the spec-first contract (root §8); the
                     directory matches the package foc.supplier.v1, as `buf lint` requires
buf.yaml, buf.gen.yaml, buf.lock
                     buf module config, generation config, pinned googleapis dep (D-039)
seed/                first-boot CSV load; depends on the domain, not vice versa
migrations/          golang-migrate .up/.down pairs, applied at startup from
                     cmd/api/main.go
```

`internal/httpapi` no longer exists — see Gotchas.

## Local development

Port **8082** (root §3, unchanged — same port, gRPC instead of HTTP/JSON on
it). Env vars `PORT`, `SUPPLIER_DB_URL`, `SEED_CSV_PATH`, `SUPPLIER_DISABLE_REFLECTION`
(unset/default leaves reflection on; any non-empty value turns it off — a
Copilot review on PR #8 flagged reflection being unconditional), with placeholders
in the root `.env.example` and this folder's own.

```bash
go run ./cmd/api     # from supplier-service/; migrates then seeds on startup
go test ./...        # unit + in-process gRPC tests, no Docker needed
docker compose up supplier-service supplier-db    # from the repo root
grpcurl -plaintext localhost:8082 list             # reflection is on by default
```

Regenerating after a `.proto` change is `buf generate` from this folder (D-039). The
tool versions and prerequisites are in the README's "Generating" section; do not
restate them here.

## Gotchas

- **The REST API is gone (2026-09-22).** `internal/httpapi`, `go-chi`, and
  `go-chi/cors` were removed; `proto/foc/supplier/v1/supplier.proto` is now the
  service's only contract. This **breaks** two things outside this folder
  that this change does not fix, because they are not this folder's to fix
  (root §3): the gateway's `/api/suppliers/*` REST proxy (D-027 assumed a
  REST downstream) needs a gRPC client instead, and the prototype frontend's
  direct `fetch()` calls to `:8082` need it too — plus browsers cannot speak
  raw gRPC at all, so the browser-facing path needs grpc-web or a
  transcoding layer, which is larger than a one-file change.
- **`is_available` has no "omit for default true" behavior anymore.** The
  old REST DTO's `*bool` let a create/update request omit the field and get
  `true`. Proto3 scalar `bool` has no presence bit, so every caller must set
  it explicitly now.
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
- **Admin auth trusts a gateway-injected `x-user-role` metadata value** (D-038).
  It is only as strong as the gateway stripping client-supplied identity and
  nothing but the gateway being able to reach this service — see the Owner
  section for why neither holds yet. Manual testing with `grpcurl -H
  "x-user-role: ADMIN"` works precisely because of that gap.
