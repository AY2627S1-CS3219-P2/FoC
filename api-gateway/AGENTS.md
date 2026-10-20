# API Gateway

<!-- AI Assistance Disclosure:
     Tool: Claude Code (model: Opus 5), date: 2026-09-19
     Scope: Rewritten when the gateway went from scaffold to working, for
       D-023 (RS256/JWKS), D-024 (no Redis), D-027 (route surface) and
       D-028 (code-first).
     2026-09-22: dependency list and the CORS entry under "Still open".
     2026-10-10 (Claude Code, Opus 5.5): the two D-043 OTP auth routes.
     Author review: Nigeltzy — Checked the output of this constantly, as this
     is constantly being updated as needed alongside development -->

The single public entry point (**D-010**). Every request from the UI arrives
here; `user-service`, `supplier-service`, `order-service` and `credit-service`
are not publicly reachable. The gateway verifies the access token's RS256
signature, checks it has not been revoked, passes the verified identity on,
and forwards the request over HTTP: **D-055** keeps this hop on REST, with
gRPC on hold.

Owner: **Nigeltzy** (**D-026**).

Root `AGENTS.md` applies in full — this file only adds what is local. The
decision this folder implements is written up in
[`../ai/decisions/D-010-api-gateway-auth.md`](../ai/decisions/D-010-api-gateway-auth.md).

**Status: working barebones.** Verifies RS256 tokens against user-service's
JWKS and the revocation keys in Redis, terminates the six auth routes, and
proxies the four service prefixes with claim-header strip-and-inject. Unit,
router and Redis integration tests pass.

## What this folder owns, and what it must not

- It owns **transport and admission**: is this caller who they claim to be, and
  where does the request go.
- It owns **no business rules**. Nothing about errands, credits, suppliers or
  matching belongs here. If a rule needs to know what an errand *is*, it is the
  owning service's, not the gateway's.
- It is **not** an issuer. `user-service` crafts and validates tokens
  (**D-012**); the gateway holds only a public key and *cannot* mint one
  (**D-023**). Do not introduce a private key or a shared secret here.
- It **reads Redis and never writes it**. After a token verifies, it reads the
  two revocation keys user-service writes, `jti:<jti>` and
  `suspended:uid:<sub>`: a logged-out token, or one issued at or before its
  account's suspension, gets 401. When Redis doesn't answer, the request gets
  503 and is not forwarded. user-service is the keys' only writer, so no
  non-test code here calls `SET`, `DEL`, `KEYS` or `SCAN`; only the
  integration test writes keys, standing in for user-service.
- It **owns the refresh token's cookie**. `internal/proxy/session_cookie.go`
  moves the token between user-service's JSON bodies and an `HttpOnly` cookie,
  so a page script can never read it. user-service's contract is unchanged —
  it still requires `refreshToken` in the body and the gateway supplies it.
- No shared auth package with the services (root §4.2). If the gateway and a
  service both parse a claim, the duplication is correct.

## The route surface (D-027, amended by D-043 and D-056)

```
TERMINATED (public, no token required)
  POST /auth/register  /auth/login  /auth/refresh  /auth/logout
  POST /auth/resend-otp  /auth/verify-otp     (D-043)
       -> rewritten onto user-service's /api/v1/users/*

PROXIED (token verified, headers stripped then injected, bearer token kept)
  /api/users/*              -> USER_BASE_URL     /api/v1/users/*
  /api/suppliers/*          -> SUPPLIER_BASE_URL /suppliers/*
  /api/orders/*             -> ORDER_BASE_URL    /*
  /api/credits/accounts/*   -> CREDIT_BASE_URL   /api/v1/credits/accounts/*  (D-056)
  (nothing else under /api/credits is routed: credit-service's reservation
   routes are for order-service alone)

  GET /healthz      the gateway's own
```

The browser's URLs deliberately do not mirror the internal service paths.

## Adding your service to the gateway

This is meant to be a small, local change:

1. Add a field to `Downstream` in `internal/config/config.go` and an entry in
   its `required` map.
2. Add one line to `serviceRoutes` in `internal/httpapi/router.go`.
3. Add the env var to `.env.example` (here and at the root) and to
   `compose.yaml`.

Token verification, the revocation check, the header strip, the injection
and the 401/503 handling all come for free. Every service row keeps the
bearer token (`proxy.NewRetainingToken`) so the service can verify it
itself. You should not need to touch `internal/auth` or `internal/proxy`.

## The line not to break

Three checks run before anything is forwarded. Removing any of them lets a
caller past the gateway that should not get there.

- **The claim-header strip.** `internal/proxy`'s `Rewrite` deletes `X-User-Id`
  and `X-User-Role` from every outbound request before injecting its own.
  Today every service takes the caller from the verified token instead and
  ignores the headers, so the strip is a second line: it keeps a forged header
  from ever reaching a service that might read it again.
  `TestUnauthenticatedRequestGetsNoClaimHeaders` and
  `TestPassthroughPreservesAuthorization` in `internal/proxy/proxy_test.go`
  fail if the delete is removed. `TestClientCannotEscalateViaHeader` does not:
  on an authenticated route the injected values overwrite the forged ones
  anyway.
- **`rejectDotSegments`**, ahead of every route, answers 404 to a path with a
  `.` or `..` segment, raw or percent-encoded. Without it,
  `/api/credits/accounts/../reservations` would reach credit-service's
  reservation routes, which are for order-service alone.
  `TestDotSegmentsAreRejectedOnEveryRoute` fails if it is removed.
- **The revocation check** in `RequireToken`. Without it, a logged-out or
  suspended token keeps working until its `exp`, up to 15 minutes, on every
  service that doesn't check the keys itself.
  `TestRevokedTokenNeverReachesAService` and
  `TestRevocationCheckUnavailableAnswers503` fail if it is removed or made to
  fail open.

If any of these tests fails, stop.

One gap is known: the gateway accepts a token up to 5 s past its `exp` (clock
skew), and a revocation key can expire at about that `exp`, so a revoked token
can pass for those 5 s until user-service keeps each key 5 s longer. Keep
`clockSkew` at 5 s or less.

## Layout

Follows root §6's layering, minus the database layers — the gateway has no
domain and no Postgres. `internal/auth` and `internal/proxy` have no
`supplier-service` counterpart.

```
cmd/api/              process wiring: config, verifier, router, shutdown
internal/config/      env read once, passed down (root §4.4)
internal/auth/        RS256 verification against the JWKS (D-023)
internal/revocation/  reads the two revocation keys from Redis; read-only
internal/proxy/       claim-to-header translation and forwarding (D-055)
internal/httpapi/     router, route table and the bearer-token middleware
api/                  openapi.yaml - empty; see D-028
```

## Local development

Module path `foc/api-gateway`. Port **8080**, provisional (D-018). Env vars are
in `.env.example`. Every one is required; the process exits
with a sorted list of what is missing rather than guessing.

```bash
go run ./cmd/api      # needs a filled .env
go test ./...
go test -tags=integration ./...   # revocation against a real Redis; needs Docker
```

`JWKS_URL` is fetched **lazily**, on the first token that needs a key - not at
boot. The gateway therefore starts fine with `user-service` absent, and answers
`503` on authenticated routes until it appears (root §5: never assume startup
order). The auth routes forward to `user-service` too, so they answer `502`
while it is down.

`REDIS_URL` is connected to lazily too: the gateway starts without Redis and
answers `503` on authenticated routes until Redis answers, then works with no
restart. It must name the same Redis and database user-service writes to, or
no revoked token is ever found.

## Dependencies

Four, all deliberate (root §2):

- `github.com/go-chi/chi/v5` - the recorded router (**D-002**).
- `github.com/golang-jwt/jwt/v5` - RS256 verification. Hand-rolling JWT
  signature checking is the kind of thing that goes quietly wrong, so the
  stdlib alone is not the right call here.
- `github.com/redis/go-redis/v9` - the revocation check's one `MGET`, at the
  version order-service and user-service use.
- `github.com/testcontainers/testcontainers-go` - the integration test's real
  Redis only; nothing in the binary imports it.

`go-chi/cors` was added on 2026-09-22 and removed the same day: the browser is
same-origin with the gateway now, so there is no CORS policy to run. JWKS
parsing and the refresh cookie are both stdlib.

## Still open

- **`api/openapi.yaml` is empty.** D-028 records the deliberate deviation from
  D-005's spec-first rule, to be **backfilled** before the gateway is treated
  as a stable contract. It is not an exemption for anyone else.
