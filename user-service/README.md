<!--
AI Assistance Disclosure:
Tool: Codex (GPT-5), date: 2026-09-21
Scope: Added user-service startup, API, and testing documentation.
Author review: Edited details from scaffold generated
-->

# User Service

The user service provides account registration, authentication, profile
management, session handling, and JWT public-key discovery. It listens on port
`8081` by default when `PORT=8081` is set.

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

2. Set environment variables
You may do this via a `.env` file or directly in your terminal. 

Replace the database URL with the connection string for your local 
PostgreSQL instance:

```bash
export PORT=8081
export JWT_KEYSET_PATH='./keys.json'
export JWT_ACCESS_TOKEN_TTL='15m'
export JWT_REFRESH_TOKEN_TTL='168h'
export REDIS_URL='redis://redis:6379/0'
export DB_USER='postgres'
export DB_PW='postgres'
export DB_URL='postgres://USER:PASSWORD@localhost:5432/user_service?sslmode=disable'
```

3. Configure the initial admin account

For the first startup, set `INITIAL_ADMIN_EMAIL`, `INITIAL_ADMIN_USERNAME`, and
`INITIAL_ADMIN_PASSWORD` in your `.env` file or terminal environment. These
values are used to create the initial `ADMIN` account when the database has no
admin account yet. Use a strong, unique password and keep these credentials out
of version control and production logs.

4. Start the service

You can start the service using either of the following methods:

* Run `docker compose up --build` if you are running from the 
root directory, or `docker compose up --env-file ./.env up --build`
if you are running from the `user-service/` directory.

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
| `GET` | `/api/v1/health` | None | Returns `200` with `{"status":"ok"}` when the database health check succeeds; otherwise returns `503`. |
| `POST` | `/api/v1/users/register` | None | JSON body: `email`, `username`, `password` (minimum 8 characters). Returns `201`; duplicate email or username returns `409`. |
| `POST` | `/api/v1/users/login` | None | JSON body: `identifier` (username or email) and `password`. Returns an `accessToken` and `refreshToken`; invalid credentials return `401`. |
| `POST` | `/api/v1/users/refresh` | None | JSON body: `refreshToken`. Rotates the refresh session and returns a new token pair; invalid, expired, or compromised tokens return `401`. |
| `POST` | `/api/v1/users/logout` | Bearer access token | JSON body: `refreshToken`. Invalidates the access token and refresh session; success returns `204`. Redis invalidation failure returns `500`. |
| `GET` | `/api/v1/users/{uid}` | Bearer access token | Returns the public profile for the UUID in `{uid}`; an unknown user returns `404`. |
| `PUT` | `/api/v1/users/{uid}` | Bearer access token | JSON body may contain `username` and/or `password`. The caller may update their own profile, or an admin may update any profile. Returns the updated profile. |
| `PATCH` | `/api/v1/users/{uid}/status` | Bearer access token, `ADMIN` role | JSON body: `{"status":"ACTIVE"}` or `{"status":"SUSPENDED"}`. Returns `200` when the status is updated. |
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
