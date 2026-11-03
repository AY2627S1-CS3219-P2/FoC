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
export OTP_DELIVERY='log'
```

Access-token revocation keys are checked on user-service's protected profile
and status routes after JWT verification. Logout keeps each `jti:<jti>` key for
the token's remaining lifetime plus the 5-second JWT clock-skew allowance;
suspension keeps each `suspended:uid:<uuid>` key for `JWT_ACCESS_TOKEN_TTL` plus
that allowance. Redis read failures fail closed with HTTP 500.

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

5. Read the verification code

Registration sends a one-time code that `POST /api/v1/users/verify-otp` checks.
`OTP_DELIVERY=mailgun`, the default when it is unset, emails the code and needs
`MAILGUN_API_KEY`, `MAILGUN_DOMAIN` and `MAILGUN_SENDER_EMAIL`.
`OTP_DELIVERY=log`, which Compose uses unless `.env` says otherwise, writes the
code to the service log instead, as a `WARN` entry with the `email` and `otp`:

```bash
docker compose logs user-service
```

With `go run ./cmd/api`, the entry appears in that terminal. Use `log` for local
development only: anyone who can read the log can verify any account.

## 2. API calls

All paths below are relative to `http://localhost:8081`. You may refer to the [openapi.yaml](./api/openapi.yaml) for more information.

| Method | Path | Auth | Remarks |
|---|---|---|---|
| `GET` | `/api/v1/health` | None | Returns `200 {"status":"ok"}` only when PostgreSQL and Redis are healthy; otherwise `503 {"status":"unhealthy"}`. |
| `POST` | `/api/v1/users/register` | None | JSON body: `email` ending in `@u.nus.edu`, alphanumeric `username`, and a 8–128 character password containing upper- and lowercase letters plus a digit. Returns `201` with an empty body; validation `400`, duplicate `409`, oversized body `413`, too many OTP requests `429` (a fourth register or resend call for that email before its 10-minute count window clears; no account is created), or unavailable service `500` (including a failed email send; the account is kept and `resend-otp` sends a new code). |
| `POST` | `/api/v1/users/login` | None | JSON body: `identifier` (username or email) and `password`. Returns an `accessToken` and `refreshToken`; errors are `400`, invalid credentials `401`, `403` with `{"error":"account suspended"}` or `{"error":"ACCOUNT_UNVERIFIED"}`, oversized body `413`, or `500`. |
| `POST` | `/api/v1/users/resend-otp` | None | JSON body: `email`. Sends a new code and invalidates the previous one; returns `200 {}`. Errors: missing or invalid email `400`; unknown email `404`; account already verified `409`; a fourth register or resend call for that email before its count clears, 10 minutes after the last call, `429`; oversized body `413`; Redis or email failure `500`. |
| `POST` | `/api/v1/users/verify-otp` | None | JSON body: `email` and `otp`. Activates an unverified account and queues its `user.registered` event; returns `200 {}`. Errors: missing field, invalid email, or wrong code `400`; no code stored (expired, never sent, already used, or discarded after five wrong guesses) `404`; code matched but the account is not unverified `409`; the email's code count over three (as for resend), or the sixth guess at one code, which discards it, `429`; oversized body `413`; `500`. |
| `POST` | `/api/v1/users/refresh` | None | JSON body: `refreshToken`. Rotates the refresh session and returns a new token pair; invalid, expired, or compromised tokens return `401`; concurrent refreshes return `429`. |
| `POST` | `/api/v1/users/logout` | Bearer access token | JSON body: `refreshToken`. Invalidates the access token and refresh session; success returns `204`; errors include `400`, `401`, `413`, and `500`. |
| `GET` | `/api/v1/users/{uid}` | Bearer access token | Returns a full profile to administrators and a restricted profile to other callers. Unknown or malformed user IDs return `404`. |
| `PUT` | `/api/v1/users/{uid}` | Bearer access token | JSON body may contain `username`, `phone_num` (at most 20 characters), `currentPassword`, and `newPassword`. The caller may update their own profile, or an admin may update any profile. Returns the updated profile. |
| `PATCH` | `/api/v1/users/{uid}/status` | Bearer access token, `ADMIN` role | JSON body: `{"status":"ACTIVE"}` or `{"status":"SUSPENDED"}`. Returns `200` when the status is updated. **`403`** also means the target is an admin account or the caller's own account: no admin account is suspended through the API (D-118, D-119); the refusal is logged. `409` means either the last active admin would be suspended or the account has not verified its email; other errors include `400`, `401`, `404`, `413`, and `500`. Each change is recorded in the audit log in the same transaction. |
| `GET` | `/api/v1/users` | Bearer access token, `ADMIN` role | The admin's account search and list (D-121 to D-123): `q` matches anywhere in the username or email, ignoring case and treating `%` and `_` literally; `status` is `ACTIVE`, `SUSPENDED` or `UNVERIFIED`; `limit` (at most 50, the default) and `offset`. Sorted by username. Returns `{items, total, limit, offset}`; each item has `uid`, `username`, `email`, `phone_num`, `account_role`, `account_status`, `date_created` and `last_login_date`. Bad query `400`, student `403`. |
| `GET` | `/api/v1/users/audit-log` | Bearer access token, `ADMIN` role | The admin audit log, newest first (D-110): `action`, `outcome`, `actor_id`, `target_id`, `from`, `to` (RFC 3339 or `YYYY-MM-DD`, `to` exclusive), `limit` and `offset`. Returns `{items, total, limit, offset}`; any admin reads all of it. See *Admin audit log* below. |
| `GET` | `/.well-known/jwks.json` | None | Returns the active and retired public JWT keys. The endpoint is unauthenticated so token-verifying services can read it over the private network; it is not published to the host. |
<!-- AI Assistance Disclosure: Codex (GPT-5), 2026-10-19 — documents D-078's two 409 responses. -->

When running with Compose, `user-service` has no `ports:` entry and is not
reachable directly from the host. Call its public routes through the gateway:
`/auth/register`, `/auth/login`, `/auth/refresh`, `/auth/logout`,
`/auth/resend-otp`, and `/auth/verify-otp`, plus `/api/users/...` for the
remaining routes. This follows D-027 and D-043. Through the gateway, the
refresh token travels in the `foc_refresh` cookie rather than the JSON body
(D-033). The direct paths in the table are for a standalone local
`go run ./cmd/api` instance.

For protected endpoints, send the token as follows:

```http
Authorization: Bearer <access-token>
```

### Admin audit log

Every admin action on an account is recorded in `admin_audit` (D-106 to D-115;
the shapes are in `ai/decisions/admin-dashboard-design.md`):

| Action | When |
|---|---|
| `account.suspend`, `account.reactivate` | `PATCH /users/{uid}/status`; the row holds the status before and after |
| `account.password_reset` | an admin changes another account's password; the row holds no value (D-111) |
| `account.update` | an admin changes another account's username or phone; the row holds the new values |

- The change and its `succeeded` row commit in **one transaction** (D-108). The
  Redis suspension key and the session revocation happen outside it; if one
  fails, the status is not saved and nothing is logged as succeeded.
- A **refused or failed** attempt (a suspension of an admin, an unverified
  account or a bad edit) is logged as `refused` or `failed` in a write of its own
  (D-109). A student's 403 and a missing token are not admin actions and are not
  logged.
- Rows are **immutable**: a trigger refuses every `UPDATE` and any `DELETE` of a
  row under 6 months old. A job at start and every 24 hours deletes the older ones
  (D-114, D-115).
- An admin editing **their own** profile is self-service and is not logged.

### Revocation keys

User-service is the only writer of the shared Redis revocation keys (D-054).
The gateway and every service that verifies tokens read them after verifying the
JWT signature and `iss`, `aud`, `exp`, and `typ` claims. Readers must take the
`uid` and `jti` only from that verified token.

| Key | Value | TTL | Written by |
|---|---|---|---|
| `jti:<access-token jti>` | `1` | The access token's remaining lifetime plus 5 seconds, the clock skew readers allow past `exp`; 5 seconds when logout arrives after `exp` but within that leeway | Logout, only when the refresh session is live and belongs to the caller; missing or revoked sessions return `204` and write nothing. |
| `suspended:uid:<uid>` | Suspension time in UTC whole seconds, formatted with `time.RFC3339Nano`, for example `2026-10-18T09:15:42Z` | `JWT_ACCESS_TOKEN_TTL` plus 5 seconds (15 minutes 5 seconds by default) | `PATCH /api/v1/users/{uid}/status` when the status becomes `SUSPENDED`. |

A verified token is revoked when its JTI key exists, when its suspension key
exists and its `iat` is at or before the stored time, or when the stored time
does not parse. Both times come from user-service's clock, so a reader's clock
drift does not affect the comparison. Reactivation does not delete the
suspension key: tokens issued in a later second pass. A password change writes
no revocation key, so readers cannot observe it. Refresh continues to reject
only a token issued before its cutoff because it also checks the refresh session
that the same invalidation revokes.

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
