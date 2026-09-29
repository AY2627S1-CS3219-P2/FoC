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

<!--
AI Assistance Disclosure:
Tool: Codex (GPT-5), date: 2026-09-29
Scope: Updated local startup and transport documentation for gRPC plus JWKS HTTP.
Author review: ZI YANG - verified correctness
-->

# User Service

The user service provides account registration, authentication, profile
management, session handling, and JWT public-key discovery. Its user API and
health protocol use gRPC on `PORT`; its one HTTP exception serves JWKS on the
fixed internal port `8085`.

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

The gRPC service is then available at `localhost:8081`. JWKS is served at
`http://localhost:8085/.well-known/jwks.json`.

## 2. Service contracts

The generated user API is defined by
[`proto/user/v1/user.proto`](./proto/user/v1/user.proto). It exposes these
unary RPCs through `user.v1.UserService`:

- `Register`
- `Login`
- `GetProfile`
- `UpdateProfile`
- `UpdateStatus`
- `RefreshSession`
- `Logout`

The API gateway supplies verified `x-user-id` and `x-user-role` metadata for
protected profile and status RPCs. `Logout` receives the original bearer value
through `authorization` metadata. `RefreshSession`, `Register`, and `Login`
remain unauthenticated RPCs.

The process also registers the standard `grpc.health.v1.Health` service. Its
whole-process and `user.v1.UserService` checks report `SERVING` only when both
PostgreSQL and Redis are reachable internally by the service.

JWKS remains HTTP because JWT verifiers discover keys through the standard
`GET /.well-known/jwks.json` path. This listener and the gRPC listener are
internal service surfaces intended to be reached through the API gateway, not
directly by browser clients. The historical
[`api/openapi.yaml`](./api/openapi.yaml) remains the source record from which
the protobuf contract was transcribed.

## 3. Test the service

Run the unit and transport tests from `user-service/`:

```bash
go test ./...
```

Run static checks:

```bash
gofmt -d $(rg --files -g '*.go')
go vet ./...
```

The tests use in-memory fakes, in-process gRPC connections, and HTTP test
servers where possible, so the standard test command does not require
PostgreSQL, Redis, or generated JWT keys. The service itself does require those
dependencies when started with `go run ./cmd/api`.
