# Supplier Service

Manages campus suppliers, facilities, and pickup locations from which
errands may be requested (product backlog FR **F2**). Owns its own
PostgreSQL database — no other service reads from or writes to it
directly.

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

| Method | Path | Auth | Notes |
|---|---|---|---|
| GET | `/health` | none | container readiness |
| GET | `/suppliers?category=&q=` | none | list/filter/search |
| GET | `/suppliers/{id}` | none | full detail |
| POST | `/suppliers` | ADMIN | create |
| PUT | `/suppliers/{id}` | ADMIN | update |
| DELETE | `/suppliers/{id}` | ADMIN | soft-delete (see below) |

### Auth (interim)

The team hasn't finalized how identity/role is passed between services
yet. Admin endpoints currently check a plain `X-User-Role: ADMIN` header
(`internal/middleware/auth.go`) purely so there's something to develop
and test against. Swap `HeaderRoleExtractor` for a real JWT/gateway-based
extractor once that's decided — no other code needs to change.

### Delete is a soft delete (interim)

F2.2.3 requires that a supplier referenced by a non-terminal errand can't
be deleted. That check depends on the Order Service, which doesn't exist
yet. Until that integration exists, `DELETE /suppliers/{id}` always
soft-deletes (sets `deleted_at`, flips `is_available` to false) instead
of removing the row — so nothing an order already points to can vanish
underneath it. Revisit once Order Service can be queried.

## Tests

```bash
go test ./...
```

Unit tests cover the service layer (validation, duplicate rejection,
soft delete) against an in-memory fake repository, and the CSV seed
parser against the actual template seed file.
