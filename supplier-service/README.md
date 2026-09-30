<!--
AI Assistance Disclosure:
Tool: Claude Code (model: Sonnet 5), date: 2026-09-22
Scope: Rewritten (>half the file changed): API section moved from REST
  routes to gRPC RPCs, new Gotchas entries for the REST removal and the
  is_available presence change.
Author review: PENDING — <reviewer to complete>
-->

# Supplier Service

Manages campus suppliers, facilities, and pickup locations from which
errands may be requested (product backlog FR **F2**). Owns its own
PostgreSQL database — no other service reads from or writes to it
directly.

**Interface: gRPC only, since 2026-09-22.** This service's entire public
contract — what the gateway calls on behalf of browsers, and what other
backend services (e.g. Order Service, for F3.1.3.1's supplier validation)
call directly — is `proto/foc/supplier/v1/supplier.proto`. There is no REST
API anymore; see [Gotchas](#gotchas) for what that breaks until the
gateway and frontend catch up.

## Requirements covered

| FR | Behaviour |
|---|---|
| F2.1.1–F2.1.3 | Supplier record fields; listing filtered by category and searched by name; full detail on selection |
| F2.2, F2.2.1–F2.2.3 | Admin create/update/delete with validation, duplicate (name+location) rejection, and soft-delete on removal |
| F2.3, F2.3.1 | Seeds itself from `data/csv/supplier-seed-data.csv` on first boot |

## Running locally

```bash
go run ./cmd/api
```

Requires a Postgres instance reachable at `SUPPLIER_DB_URL` (see
`.env.example`). Migrations run automatically on startup, followed by a
one-time seed from the CSV if the table is empty.

Via Docker Compose (from the repo root):

```bash
docker compose up supplier-service supplier-db
```

## API

`proto/foc/supplier/v1/supplier.proto` is the contract. Go server/client code
is generated into `internal/gen/supplier/v1/` — **never hand-edit that
output**; change the `.proto` and regenerate.

### Generating (`buf`, D-039)

From `supplier-service/`:

```bash
buf generate
```

`buf.yaml` makes `proto/` the module and depends on `buf.build/googleapis/googleapis`
(pinned in `buf.lock`) for the `google.api.http` protos. `buf.gen.yaml` runs the
**local** Go plugins and writes into `internal/gen/`. The first `buf generate` on a
machine downloads the pinned dependency from buf.build; `buf dep update` refreshes
`buf.lock`.

Both plugins must be on `PATH`. These are the versions of the last run; nothing
enforces them, and the generated headers record what was used:

| Tool | Version | Get it |
|---|---|---|
| `buf` | 1.73.0 | `brew install buf`, or a release binary from github.com/bufbuild/buf |
| `protoc-gen-go` | v1.36.12 | `go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.12` |
| `protoc-gen-go-grpc` | v1.6.2 | `go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.2` |

The generated headers read `protoc (unknown)` because `buf` compiles the protos
itself; the rest of the output matches what `protoc` produced.

| RPC | Public URL via the gateway (D-037) | Auth | Notes |
|---|---|---|---|
| `ListSuppliers` | `GET /api/suppliers?category=&q=` | none yet | bare JSON array; filter by category and/or search by name |
| `GetSupplier` | `GET /api/suppliers/{id}` | none yet | full detail |
| `CreateSupplier` | `POST /api/suppliers` | ADMIN | JSON body |
| `UpdateSupplier` | `PUT /api/suppliers/{id}` | ADMIN | JSON body |
| `DeleteSupplier` | `DELETE /api/suppliers/{id}` | ADMIN | soft-delete (see below) |

Each RPC has its own response message (D-040): `Get`, `Create` and `Update` return `{supplier: {...}}` over gRPC, and `Delete` returns an empty message. Their `google.api.http` rules use `response_body: "supplier"`, so the browser-facing JSON is the flat supplier object.

The URLs are `google.api.http` options in the `.proto`, metadata for the gateway's generator: this service serves no REST itself. All five are reachable from the browser; the three writes are admin-only. Reads do not require authentication yet, which is undecided.

JSON names are snake_case (`location_description`, `is_available`, `created_at`), pinned in the `.proto` with `json_name` (D-036), so the shape the gateway emits and the frontend reads does not depend on gateway settings. Errors are gRPC status codes with a readable message; turning them into `{"error": "<message>"}` is the gateway's job.

`grpc.health.v1.Health/Check` replaces the former `GET /health` for
container readiness. Server reflection is on by default, so `grpcurl` (or
any reflection-aware client) needs no local copy of the `.proto` — set
`SUPPLIER_DISABLE_REFLECTION` to any non-empty value to turn it off:

```bash
grpcurl -plaintext localhost:8082 list
grpcurl -plaintext -d '{}' localhost:8082 foc.supplier.v1.SupplierService/ListSuppliers
grpcurl -plaintext -H "x-user-role: ADMIN" -d '{"supplier":{"name":"...", ...}}' \
  localhost:8082 foc.supplier.v1.SupplierService/CreateSupplier
```

### Auth (D-038)

The role comes from user-service: it sets `users.account_role` and puts it in the
access token's `role` claim at login. The gateway verifies that token, discards any
identity the client sent, and injects the verified role as gRPC metadata; this service
reads `x-user-role` (`internal/middleware/auth.go`), trusts it, and never sees the token.
`CreateSupplier`, `UpdateSupplier` and `DeleteSupplier` require exactly `ADMIN`; a missing
or repeated value counts as no role. Reads need no role here.

**Not sound yet:** the gateway does not yet inject or strip identity for gRPC, and
`compose.yaml` still publishes `:8082`, so a direct caller can claim `ADMIN`. Both are
listed in `AGENTS.md`.

### Delete is a soft delete (interim)

F2.2.3 requires that a supplier referenced by a non-terminal errand can't
be deleted. That check depends on the Order Service, which doesn't exist
yet. Until that integration exists, `DeleteSupplier` always soft-deletes
(sets `deleted_at`, flips `is_available` to false) instead of removing
the row — so nothing an order already points to can vanish underneath
it. Revisit once Order Service can be queried.

## Gotchas

- **The REST API is gone.** `internal/httpapi`, `go-chi`, and `go-chi/cors`
  were removed 2026-09-22 in favor of gRPC as the service's only
  interface (see `ai/decisions.md`). This **breaks** the gateway's
  `/api/suppliers/*` REST proxy (D-027) and the prototype frontend's
  direct `fetch()` calls to `:8082` — both spoke REST/JSON. Neither is
  fixed here: `api-gateway` needs a gRPC client to this service instead
  of an HTTP passthrough, and browsers can't speak raw gRPC at all, so
  the browser-facing path needs grpc-web or a transcoding layer. Both are
  other people's folders (root `AGENTS.md` §3) — flagged, not fixed, by
  this change.
- **`is_available` has no "omit for default true" behavior anymore.**
  The REST DTO's `*bool` let a create/update request omit `is_available`
  and get `true`. Proto3 scalar `bool` has no presence bit, so the field
  is always sent; the server no longer has a way to tell "omitted" from
  "explicitly false." Every caller must set it explicitly.
- The CSV encoding and Dockerfile-build-context gotchas from before this
  change are unaffected — see git history if you need them.

## Tests

```bash
go test ./...
```

Unit tests cover the service layer (validation, duplicate rejection,
soft delete) against an in-memory fake repository, the CSV seed parser
against the actual template seed file, the admin-role gRPC interceptor,
and the gRPC server end to end (via `bufconn`, no Docker needed) —
list/filter, get, create, duplicate/validation errors, the admin gate,
update, and soft-delete-then-not-found.
