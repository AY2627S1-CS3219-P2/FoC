# API Gateway

<!-- AI Assistance Disclosure:
     Tool: Claude Code (model: Opus 5), date: 2026-09-19
     Scope: Rewritten when the gateway went from scaffold to working, for
       D-023 (RS256/JWKS), D-024 (no Redis), D-027 (route surface) and
       D-028 (code-first).
     2026-09-22: dependency list and the CORS entry under "Still open".
     Author review: Nigeltzy — Checked the output of this constantly, as this
     is constantly being updated as needed alongside development -->

The single public entry point (**D-010**). Every request from the UI arrives
here; `user-service`, `supplier-service`, `order-service` and `credit-service`
are not publicly reachable. The gateway verifies the access token's RS256
signature, translates claims into HTTP headers, and forwards over synchronous
REST (**D-013**, as amended by **D-024**).

Owner: **Nigeltzy** (**D-026**).

Root `AGENTS.md` applies in full — this file only adds what is local. The
decision this folder implements is written up in
[`../ai/decisions/D-010-api-gateway-auth.md`](../ai/decisions/D-010-api-gateway-auth.md).

**Status: working barebones.** Verifies RS256 tokens against user-service's
JWKS, terminates the four auth routes, and proxies the four service prefixes
with claim-header strip-and-inject. Unit and router tests pass. Never yet
exercised against a real `user-service`, because there isn't one.

## What this folder owns, and what it must not

- It owns **transport and admission**: is this caller who they claim to be, and
  where does the request go.
- It owns **no business rules**. Nothing about errands, credits, suppliers or
  matching belongs here. If a rule needs to know what an errand *is*, it is the
  owning service's, not the gateway's.
- It is **not** an issuer. `user-service` crafts and validates tokens
  (**D-012**); the gateway holds only a public key and *cannot* mint one
  (**D-023**). Do not introduce a private key or a shared secret here.
- It is **not** a Redis client (**D-024**). It does not check revocation. A
  token is good until its `exp`; what that costs is recorded as **D-025**.
- It **owns the refresh token's cookie**. `internal/proxy/session_cookie.go`
  moves the token between user-service's JSON bodies and an `HttpOnly` cookie,
  so a page script can never read it. user-service's contract is unchanged —
  it still requires `refreshToken` in the body and the gateway supplies it.
- No shared auth package with the services (root §4.2). If the gateway and a
  service both parse a claim, the duplication is correct.

## The route surface (D-027)

```
TERMINATED (public, no token required)
  POST /auth/register  /auth/login  /auth/refresh  /auth/logout
       -> rewritten onto user-service's /api/v1/users/*

PROXIED (token verified, headers stripped then injected)
  /api/users/*      -> USER_BASE_URL
  /api/suppliers/*  -> SUPPLIER_BASE_URL
  /api/orders/*     -> ORDER_BASE_URL
  /api/credits/*    -> CREDIT_BASE_URL

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

Token verification, the header strip, the injection and the 401/503 handling
all come for free. You should not need to touch `internal/auth` or
`internal/proxy`.

## The line not to break

`internal/proxy`'s `Rewrite` deletes `X-User-Id` and `X-User-Role` from every
outbound request before injecting its own. **D-022 rests entirely on that.**
Remove it and any caller can send `X-User-Role: ADMIN` with an ordinary token
and be believed by every service downstream.

`TestClientCannotEscalateViaHeader` in `internal/httpapi/router_test.go` exists
to catch exactly that regression. If it fails, stop.

## Layout

Mirrors `supplier-service` package for package (root §6), minus the database
layers — the gateway has no domain and no Postgres.

```
cmd/api/              process wiring: config, verifier, router, shutdown
internal/config/      env read once, passed down (root §4.4)
internal/auth/        RS256 verification against the JWKS (D-023)
internal/proxy/       claim-to-header translation and forwarding (D-013)
internal/httpapi/     router, route table and the bearer-token middleware
api/                  openapi.yaml - empty; see D-028
```

There is no `internal/revocation/`: D-024 leaves it with nothing to do.

## Local development

Module path `foc/api-gateway`. Port **8080**, provisional (D-018). Env vars are
in `.env.example`, and every one is required; the process exits with a sorted
list of what is missing rather than guessing.

```bash
go run ./cmd/api      # needs a filled .env
go test ./...
```

`JWKS_URL` is fetched **lazily**, on the first token that needs a key - not at
boot. The gateway therefore starts fine with `user-service` absent, and answers
`503` on authenticated routes until it appears (root §5: never assume startup
order). The auth routes keep working throughout, since they carry no token.

## Dependencies

Two, both deliberate (root §2):

- `github.com/go-chi/chi/v5` - the recorded router (**D-002**).
- `github.com/golang-jwt/jwt/v5` - RS256 verification. Hand-rolling JWT
  signature checking is the kind of thing that goes quietly wrong, so the
  stdlib alone is not the right call here.

`go-chi/cors` was added on 2026-09-22 and removed the same day: the browser is
same-origin with the gateway now, so there is no CORS policy to run. JWKS
parsing and the refresh cookie are both stdlib.

## Still open

- **`api/openapi.yaml` is empty.** D-028 records the deliberate deviation from
  D-005's spec-first rule, to be **backfilled** before the gateway is treated
  as a stable contract. It is not an exemption for anyone else.
- **Never exercised against a real `user-service`.** Every test mints its own
  RS256 tokens against a stub JWKS. The first contact with Zi Yang's service
  will find things.
