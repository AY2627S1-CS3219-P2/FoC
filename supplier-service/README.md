# Supplier Service

Manages campus suppliers, facilities, and pickup locations from which
errands may be requested (product backlog FR **F2**). Owns its own
PostgreSQL database — no other service reads from or writes to it
directly.

## Requirements covered

| FR | Behaviour |
|---|---|
| F2.1.1–F2.1.3 | Supplier record fields; listing filtered by category and searched by name; full detail on selection |
| F2.2, F2.2.1–F2.2.3 | Admin create/update/delete with validation, duplicate (name+location) rejection, and soft-delete on removal, refused while an unfinished errand references the supplier |
| F2.3, F2.3.1 | Seeds itself from `data/csv/supplier-seed-data.csv` on first boot |

## Running locally

```bash
go run ./cmd/api
```

Requires a Postgres instance reachable at `SUPPLIER_DB_URL` (see
`.env.example`). Migrations run automatically on startup, followed by a
one-time seed from the CSV if the table is empty. Deleting a supplier also
needs order-service at `ORDER_BASE_URL` (default `http://localhost:8083`);
without it every delete is refused. Create, update and delete need an ADMIN's
access token, so a host run also needs user-service's keys at `JWKS_URL`
(default `http://localhost:8081/.well-known/jwks.json`) and Redis at `REDIS_URL`
(default `redis://localhost:6379`); Compose publishes neither, so run them
yourself or publish them. Reads need neither.

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
| POST | `/suppliers` | ADMIN access token | create |
| PUT | `/suppliers/{id}` | ADMIN access token | update |
| DELETE | `/suppliers/{id}` | ADMIN access token | soft-delete; 409 while an unfinished errand references it (see below) |

### Status codes for bad input and races

| Case | Answer |
|---|---|
| An id that is not a UUID (`GET`, `PUT` or `DELETE /suppliers/not-a-uuid`) | 404 `supplier not found`, the same as an unknown id; order-service is not asked |
| An upper-case id | the same supplier as the lower-case one |
| `POST` or `PUT` body over 64 KiB, padding after the JSON included | 413 `request body too large`; nothing is saved |
| `POST` or `PUT` body that is not exactly one JSON value (trailing data after it) | 400 `invalid JSON body` |
| Two creates, or a create and an update, landing on the same name, building and location at once | one succeeds and the other gets 409 |
| `PUT` of a supplier that was deleted a moment earlier | 404; nothing is saved |

`is_available` is `false` when the body leaves it out, on `POST` and on `PUT`, and
an explicit `false` on `POST` is kept, so an admin can add a supplier that starts
unavailable. A caller that wants an available supplier must send `true`; the
frontend's form always does.

### Auth

The service verifies the caller itself (D-041, D-054). `POST`, `PUT` and
`DELETE /suppliers` need `Authorization: Bearer <access token>` from a login
through user-service. The token's RS256 signature is checked against
user-service's published keys (`JWKS_URL`), along with `iss`, `aud`, `exp` (with
no leeway, because the logout key expires at `exp`) and `typ`; then the revocation keys user-service writes to Redis (`REDIS_URL`), so
a logged-out or suspended admin's token stops working at once. The caller is
the token's `sub` and `role`. `X-User-Id` and `X-User-Role` are ignored.

| Answer | When |
|---|---|
| 401 `missing, invalid or expired access token` | no, malformed, expired, revoked or wrongly typed token; a role that is neither STUDENT nor ADMIN |
| 403 `admin role required` | a valid STUDENT token |
| 503 `sign-in cannot be checked right now; please try again` | user-service's keys cannot be loaded, or Redis cannot be reached or answers with an error |
| otherwise | the answers below |

Reads (`GET /suppliers`, `GET /suppliers/{id}`) and `GET /health` need no token
and ignore `Authorization`.

### Delete checks for unfinished errands, then soft-deletes

F2.2.3 requires that a supplier referenced by an errand that is not
finished can't be deleted. Before deleting, `DELETE /suppliers/{id}` asks
order-service at `ORDER_BASE_URL` with
`GET /suppliers/{id}/active-errands`, which counts the errands naming the
supplier that are not yet `COMPLETED`, `CANCELLED` or `EXPIRED`
(`internal/orderclient`). A supplier that doesn't exist is still a 404,
without asking.

- **Count above 0:** `409 Conflict` with
  `{"error": "supplier is referenced by an errand that is not finished"}`.
  Nothing changes.
- **order-service can't be asked** (unreachable, no answer within 3
  seconds, any status other than 200, or a malformed body): `500`. Nothing
  changes; with the count unknown, the supplier may still be in use.
- **Count 0:** soft delete (sets `deleted_at`, flips `is_available` to
  false) instead of removing the row, so nothing an order already points
  to can vanish underneath it.

order-service answers only an ADMIN's access token, so the delete handler
forwards the token it verified, as `Authorization: Bearer <token>`. The API
Gateway keeps `Authorization` on `/api/suppliers` (PR #22); without it every
admin write is a 401.

## Tests

```bash
go test ./...
go test -tags=integration ./...   # needs Docker
```

Unit tests cover the service layer (validation, duplicate rejection, the
unfinished-errand check before a soft delete) against in-memory fakes, the
token check (every refusal, forged identity headers, key fetching and
revocation) against real RS256 tokens served from an `httptest` key set, the
order-service client against an `httptest` server, and the CSV seed
parser against the actual template seed file. The integration tests run admin
create, update and delete over a real PostgreSQL and Redis, with revocation
keys written the way user-service writes them and with Redis stopped.
