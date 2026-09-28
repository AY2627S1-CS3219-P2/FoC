<!--
AI Assistance Disclosure:
Tool: Codex (GPT-5), date: 2026-09-21
Scope: Added user-service startup, API, and testing documentation.
Author review: Edited details from scaffold generated
-->

<!--
AI Assistance Disclosure:
Tool: Codex (GPT-5), date: 2026-09-28
Scope: Aligned startup and endpoint documentation with the current user-service implementation.
Author review: ZI YANG - validated correctness
-->

# User Service

The user service provides account registration, authentication, profile
management, session handling, and JWT public-key discovery. Set `PORT` before
starting it; the service has no built-in default port.

## 1. Start the service

### Prerequisites

- Go 1.26 or later
- PostgreSQL
- Redis
- Python 3 with the `cryptography` package for generating local JWT keys

The service applies the SQL migrations in `migrations/` automatically at
startup. PostgreSQL and Redis must therefore be running before the service is
started.

### Local setup

From the `user-service` directory:

1. Generate private keys

```bash
python -m pip install cryptography
python generate_jwks.py --out keys.json
```

<!-- AI-generated (edited by ZI YANG). -->
Keep `keys.json` local and out of version control. Before running Compose from
the repository root, generate it at `user-service/keys.json`; Compose mounts it
read-only at `/run/secrets/keys.json`, and the image never contains the file.

2. Export environment variables

The service reads process environment variables; it does not load a `.env`
file itself. For a host-run service, provide reachable PostgreSQL and Redis
instances and replace these example URLs with their host addresses:

```bash
export PORT=8081
export JWT_KEYSET_PATH='./keys.json'
export JWT_ACCESS_TOKEN_TTL='15m'
export JWT_REFRESH_TOKEN_TTL='168h'
export REDIS_URL='redis://localhost:6379/0'
export USER_DB_URL='postgres://postgres:postgres@localhost:5432/user?sslmode=disable'
```

3. Configure the initial admin account

For the first startup, export `INITIAL_ADMIN_EMAIL`, `INITIAL_ADMIN_USERNAME`,
and `INITIAL_ADMIN_PASSWORD`. These values create the initial `ADMIN` account
when the database has no admin account yet. Use a strong, unique password and
keep these credentials out of version control and production logs.

4. Start the service

You can start the service using either of the following methods:

* After generating `user-service/keys.json`, run `docker compose up --build`
  from either the repository root or `user-service/`. Compose discovers the
  root `compose.yaml` and root `.env`; `USER_SRV_DB_USER` and `USER_SRV_DB_PW`
  are Compose inputs, not service configuration.

OR

* Start the API from `user-service/` so the relative migration path resolves:

```bash
go run ./cmd/api
```

The service is then available at `http://localhost:8081`.

## 2. API calls

All paths below are relative to `http://localhost:8081`. You may refer to the [openapi.yaml](./api/openapi.yaml) for more information.

| Method | Path | Auth | Remarks |
|---|---|---|---|
| `GET` | `/api/v1/health` | None | Returns `200 {"status":"ok"}` only when PostgreSQL and Redis are healthy; otherwise `503 {"status":"unhealthy"}`. |
| `POST` | `/api/v1/users/register` | None | JSON body: `email` ending in `@u.nus.edu`, alphanumeric `username`, and a 8–128 character password containing upper- and lowercase letters plus a digit. Returns `201`; validation `400`, duplicate `409`, oversized body `413`, or unavailable service `500`. |
| `POST` | `/api/v1/users/login` | None | JSON body: `identifier` (username or email) and `password`. Returns an `accessToken` and `refreshToken`; errors are `400`, invalid credentials `401`, suspended account `403`, oversized body `413`, or `500`. |
| `POST` | `/api/v1/users/refresh` | None | JSON body: `refreshToken`. Rotates the refresh session and returns a new token pair; invalid, expired, or compromised tokens return `401`; concurrent refreshes return `429`. |
| `POST` | `/api/v1/users/logout` | Bearer access token | JSON body: `refreshToken`. Invalidates the access token and refresh session; success returns `204`; errors include `400`, `401`, `413`, and `500`. |
| `GET` | `/api/v1/users/{uid}` | Bearer access token | Returns a full profile to administrators and a restricted profile to other callers. Unknown or malformed user IDs return `404`. |
| `PUT` | `/api/v1/users/{uid}` | Bearer access token | JSON body may contain `username`, `phone_num` (at most 20 characters), `currentPassword`, and `newPassword`. The caller may update their own profile, or an admin may update any profile. Returns the updated profile. |
| `PATCH` | `/api/v1/users/{uid}/status` | Bearer access token, `ADMIN` role | JSON body: `{"status":"ACTIVE"}` or `{"status":"SUSPENDED"}`. Returns `200` when the status is updated; errors include `400`, `401`, `403`, `404`, `409`, `413`, and `500`. |
| `GET` | `/.well-known/jwks.json` | None | Returns the active and retired public JWT keys. This endpoint is intended to be reachable only through the API gateway. |

For protected endpoints, send the token as follows:

```http
Authorization: Bearer <access-token>
```

## 3. Test the service

Run the unit and HTTP handler tests from `user-service/`:

```bash
go test ./...
```

Run static checks:

```bash
gofmt -d $(rg --files -g '*.go')
go vet ./...
```

The tests use in-memory fakes and HTTP test servers where possible, so the
standard test command does not require PostgreSQL, Redis, or generated JWT
keys. The service itself does require those dependencies when started with
`go run ./cmd/api`.
