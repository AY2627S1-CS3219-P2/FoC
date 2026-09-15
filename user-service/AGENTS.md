# User Service

The identity authority for the platform (mandatory requirement **M2**):
accounts, credentials, sessions/tokens, profile information, and the role a
person acts in — requester, courier, or admin — including the toggle between
requester and courier. Every other service trusts the identity this one
issues and stores only a user ID alongside its own data. In return, this
service knows nothing about errands, credit balances, or the supplier
catalogue, and never stores or reasons about them.

**Status: not implemented on any branch.** This folder holds only empty
`Dockerfile` and `README.md` placeholders. Everything below is the convention
to build to, taken from `supplier-service` — not a description of code that
exists.

## Owner

TBD — one developer owns this folder (root §3); every decision flagged open
below is theirs, not an agent's.

## Boundaries

- **Other services asking about a user.** `order-service` needs "is this
  user a courier?", `credit-service` "does this user exist?", `frontend` a
  display name. Each is an HTTP call through the generated OpenAPI client,
  or an event this service publishes — never a join, never a second
  connection to this database.
- **This service asking about a user's activity.** A rule like "you may not
  switch to requester while a delivery is in flight" is an `order-service`
  question. Either call `order-service` through its generated client or hand
  the rule back to the owner; do not read its tables and do not mirror order
  state here.
- **Balances and credits belong to `credit-service`.** A profile screen
  showing a balance is composed by the caller, not by a column added here.
- **No shared auth package.** If this service and another both need to
  verify a token, the duplication is correct (root §4.2). Do not create a
  shared module or import across service modules.
- **How identity crosses a service boundary is an OPEN TEAM DECISION.**
  `supplier-service` currently guards its admin endpoints with a plain
  `X-User-Role` header, read by an injectable `RoleExtractor`
  (`supplier-service/internal/middleware/auth.go`), explicitly as an interim
  stand-in. Whether the real mechanism is a JWT verified per service,
  gateway-injected claims, or something else is the team's call — an agent
  must not pick it, and must not edit `supplier-service` to match a scheme it
  picked. The swap point is deliberately that one interface. Keep the
  issuing side here behind one interface too, so the change stays local.

## Layout

Mirror `supplier-service` package for package (root §6 and that folder's
`AGENTS.md`); module path `foc/user-service`; migrations named
`NNNNNN_<name>.{up,down}.sql`.

Which domain packages exist under `internal/`, and what each contains, is the
owner's to decide — `supplier-service` has exactly one (`internal/supplier`)
because it has one aggregate; identity may not.

Root §8 makes `api/openapi.yaml` the contract; `supplier-service` predates it
and has no `api/` directory yet. Adding one here is an interface decision: the
owner writes the spec, an agent implements against it.

## Local development

Port **8081**, database URL `USER_DB_URL` (root §3). Also the JWT settings
already present in the root `.env.example` — `JWT_SECRET`,
`JWT_ACCESS_TOKEN_TTL`, `JWT_REFRESH_TOKEN_TTL`.

```bash
go run ./cmd/api      # migrations run on startup via golang-migrate
go test ./...
go test -tags=integration ./...
```

A `user-service` / `user-db` pair in the root `compose.yaml` does not exist
yet; adding it edits a shared file, so flag it (root §3).

## Gotchas

- **Credentials live here, so this is the highest-risk code in the repo.**
  Password storage and token handling use a vetted library (e.g.
  `golang.org/x/crypto` bcrypt/argon2) — no home-rolled crypto. Never log a
  password, hash, token or `JWT_SECRET`; keep them out of DTOs and errors.
- **Role changes are named operations, not a boolean parameter.** A
  `SetRole(ctx, id, isAdmin bool)`-shaped API is control coupling (root §5);
  the requester/courier toggle and any admin grant are separate, separately
  authorised functions.
- **How the first admin comes into existence is an open decision** (tracked in
  `ai/decisions.md`) for the
  owner. If a task implies one, stop and ask rather than picking. Flag the
  security implications to the human; do not settle them yourself.
