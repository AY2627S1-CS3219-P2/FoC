# API Gateway

The single public entry point (**D-010**). Every request from the UI arrives
here; `user-service`, `supplier-service`, `order-service` and `credit-service`
are not publicly reachable. The gateway verifies the access token, checks
revocation, translates claims into HTTP headers, and forwards over synchronous
REST (**D-013**).

Root `AGENTS.md` applies in full — this file only adds what is local. The
decision this folder implements is written up in
[`../ai/decisions/D-010-api-gateway-auth.md`](../ai/decisions/D-010-api-gateway-auth.md).

**Status: scaffold only.** `go.mod`, config plumbing, process wiring and a
`/healthz` endpoint. No token verification, no revocation check, no proxying.
Each empty package's `doc.go` names what blocks it.

## What this folder owns, and what it must not

- It owns **transport and admission**: is this caller who they claim to be, is
  the token still good, and where does the request go.
- It owns **no business rules**. Nothing about errands, credits, suppliers or
  matching belongs here. If a rule needs to know what an errand *is*, it is the
  owning service's, not the gateway's.
- It is **not** an issuer. `user-service` crafts and validates tokens
  (**D-012**); the gateway only verifies signatures and forwards the auth
  endpoints. Do not craft a token in this folder.
- No shared auth package with the services (root §4.2). If the gateway and a
  service both parse a claim, the duplication is correct.

## Before writing more of this

Four things are recorded (D-010 – D-015) and several are not. The Open table in
[`../ai/decisions.md`](../ai/decisions.md) is authoritative; the ones that block
code here:

<!-- AI-generated (edited by PENDING) — items 1 and the Redis line under
     Local development rewritten for D-020. -->
1. **The Redis key spelling.** *Settled:* D-020 supersedes D-017 —
   `user-service` is the sole writer and this gateway only reads, so no
   inbound "please revoke" API is needed and `internal/revocation` is
   unblocked in principle. *Unsettled:* the team diagram writes
   `suspended:<uid>` while `user-service`'s own `AGENTS.md` writes
   `suspended:uid:<uuid>` and pins the key's TTL to the access-token lifetime.
   That document is on no branch yet, so it is not something to build against
   (root §1). The two spellings need reconciling into one recorded answer.
2. **The claim header names.** `supplier-service` reads `X-User-Role` today as
   a self-described interim stand-in. Whether the gateway adopts that name is
   the team's call, and changing `supplier-service` to match is its owner's.
3. **The auth route paths and payloads.** D-005 makes `api/openapi.yaml` the
   place for those, authored by a human (root §8).

## Layout

Mirrors `supplier-service` package for package (root §6), minus the database
layers — the gateway has no domain and no Postgres.

```
cmd/api/              process wiring: config, server, graceful shutdown
internal/config/      env read once, passed down (root §4.4)
internal/auth/        access-token verification (D-011)
internal/revocation/  blocklist and suspension checks (D-013, D-014)
internal/proxy/       claim-to-header translation and forwarding (D-013)
internal/httpapi/     router and transport-only handlers
api/                  openapi.yaml — human-authored, empty today
```

## Local development

Module path `foc/api-gateway`. Port **8080**, provisional (D-018). It *reads*
Redis on 6379; `user-service` is the sole writer (D-020, superseding D-017), so
that connection string reaches both — unlike every Postgres URL in this repo,
which stays with exactly one service. Root `AGENTS.md` §4.1 and D-003 say a
datastore has one service touching it, and D-020 is marked as still owing a
written carve-out for this. Env vars are in `.env.example`, and every one is
required; the process exits with the list of what is missing rather than
guessing.

```bash
go run ./cmd/api      # needs a filled .env
go test ./...
```

The module currently requires nothing beyond the standard library. `chi/v5` is
the recorded router (**D-002**) and should be added when the first real route
is, not before.
