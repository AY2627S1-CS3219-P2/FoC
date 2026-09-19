# Decision Record

The team's decisions, written down so they can be built against. Root
`AGENTS.md` §1 treats a decision as **made** only if it appears here, in a
merged `api/openapi.yaml`, or in code already on `main`. Anything else is still
open, and an AI tool must hand it back rather than pick an answer.

**This file is human-written.** Requirements, architecture, service boundaries,
design patterns, data schemas, API interfaces and the *reasoning* behind them
are exactly what CS3219 Appendix 2 forbids delegating. An AI tool may transcribe
a decision you have already made, and may implement one, but must not fill in a
`Rationale` cell or add a row for a question you have not answered.

Rationale matters beyond compliance: D3 and D4 grade "technical depth →
rationale for decisions is explained; trade-offs and integration considerations
addressed." These rows are the raw material for those slides.

## How to add one

Append to the table. Keep the statement short enough that someone can build
against it without asking a follow-up question. If a decision needs more than a
few lines, write it up in `ai/decisions/<id>-<slug>.md` and link it.

<!-- Rows D-010..D-015 below: AI-transcribed (Claude Code, Opus 5, 2026-09-19)
     from the team's architecture diagram and token-lifecycle write-up.
     Wording only — no Rationale cell filled, no row invented. -->
| ID | Date | Decided by | Decision | Rationale | Status |
| --- | --- | --- | --- | --- | --- |
| D-001 | 2026-09-15 | team | Backend services are written in Go | | Accepted |
| D-002 | 2026-09-15 | team | HTTP routing uses `chi/v5`; persistence uses `pgx/v5` against PostgreSQL; migrations use `golang-migrate` | | Accepted |
| D-003 | 2026-09-15 | team | Each service owns its own PostgreSQL database exclusively; no shared schema and no cross-service table access | | Accepted |
| D-004 | 2026-09-15 | team | Each service is a separate Go module (`foc/<service>`), not one root module | | Accepted |
| D-005 | 2026-09-15 | team | API contracts are spec-first: `<service>/api/openapi.yaml` is authored first, Go types and the server interface are generated from it into `internal/gen/`, and generated code is committed | | Accepted |
| D-006 | 2026-09-15 | team | CI runs on GitHub Actions | | Accepted |
| D-007 | 2026-09-15 | team | Local deployment is Docker Compose from a single root `compose.yaml` | | Accepted |
| D-008 | 2026-09-15 | team | Local port allocation: user 8081, supplier 8082, order 8083, credit 8084, frontend 3001 | | Accepted |
| D-009 | 2026-09-15 | team | Cloud provider is AWS. The compute target and IaC tool are **not** decided yet and nothing is to be scaffolded for them | | Partial |
| D-010 | 2026-09-19 | team | Every client request enters the system through an **API Gateway**. `user-service`, `supplier-service`, `order-service` and `credit-service` are not publicly reachable; only the gateway is. See [D-010 detail](decisions/D-010-api-gateway-auth.md) | | Accepted |
| D-011 | 2026-09-19 | team | Authentication uses a short-lived **access token (AT)** plus a **refresh token (RT)**. The AT is a JWT carrying `sub` (user ID), `role` (`USER` or `ADMIN`), `jti` (JWT ID) and `exp`. The RT's only purpose is to be exchanged for a new AT | | Accepted |
| D-012 | 2026-09-19 | team | `user-service` is the sole issuer of both tokens. It verifies credentials against the User DB, crafts the AT and RT, and stores the RT **hashed** in the User DB | | Accepted |
| D-013 | 2026-09-19 | team | On every subsequent request the gateway verifies the AT's signature, checks Redis for revocation, translates the claims into HTTP headers, and forwards the request to the downstream service over synchronous REST | | Accepted |
| D-014 | 2026-09-19 | team | Revocation is Redis-backed. **Logout:** `user-service` deletes the RT from the User DB and writes the AT's `jti` to a Redis blocklist. **Suspension:** `user-service` writes a `suspended:<uid>` key to Redis, and the gateway rejects any token for that `sub` issued before the suspension timestamp, ending all active sessions | | Accepted |
| D-015 | 2026-09-19 | team | The refresh exchange is routed **through the gateway** (UI → gateway → `user-service`), not direct to `user-service`. The UI therefore holds exactly one base URL | | Accepted |
| D-016 | 2026-09-17 | Nigeltzy (frontend owner) | The frontend is **React + Vite, TypeScript**. Already recorded in root `AGENTS.md` §2 and `frontend/AGENTS.md`; transcribed here so this file is complete | | Accepted |
| D-017 | 2026-09-19 | team | **The API Gateway owns Redis.** It is a separate datastore from the User DB — different engine, different process, different connection string — and holds only revocation state (the `jti` blocklist and `suspended:<uid>` keys), never credentials, refresh tokens or profile data. This resolves the D-013/D-014 clash with root `AGENTS.md` §4.1 and D-003 | | Accepted |
| D-018 | 2026-09-19 | Nigeltzy | **Provisional** local ports, "whatever works for the time being": API Gateway **8080**, Redis **6379** (its own default). To be revisited before deployment | | Provisional |

## Open — do not implement until these have a row above

Named here so an agent can point at the gap instead of guessing. Each is
graded at D2 or D3, so the answer needs a rationale written alongside it.

| Question | Blocks | Milestone |
| --- | --- | --- |
| **How `user-service` triggers a revocation now that D-017 gives Redis to the gateway.** D-014 has `user-service` writing the `jti` blocklist and the `suspended:<uid>` key directly; under D-017 it can no longer reach Redis, so it must ask the gateway. That call is a new API surface and needs a spec (D-005) | API Gateway, User Service | D2 |
| Whether the gateway is a Go service we write or an off-the-shelf product | API Gateway | D2 |
| The auth endpoint paths and payloads (login, refresh, logout) — D-005 makes this an `api/openapi.yaml` | User Service, API Gateway, `frontend/` | D2 |
| The header names the gateway translates claims into. `supplier-service` currently reads `X-User-Role` as an interim stand-in | Every service's auth guard | D2 |
| AT and RT lifetimes (`JWT_ACCESS_TOKEN_TTL`, `JWT_REFRESH_TOKEN_TTL` are placeholders in root `.env.example`) | User Service | D2 |
| Where the UI keeps the AT and RT | `frontend/` | D2 |
| Whether requester/courier is a second axis alongside D-011's `role: USER/ADMIN`, or modes of one account | User Service data model | D2 |
| The order state set and which transitions are legal | Order Service | D3 |
| How concurrent acceptance of the same errand is made safe | Order Service | D3 |
| Whether credit reservation is atomic with order persistence | Order + Credit Services | D3 |
| What happens to an errand that stalls after acceptance | Order Service | D3 |
| How credit-operation atomicity is achieved | Credit Service | D3 |
| Which interactions are asynchronous, and the broker technology | Async workflow (M6) | D3 |
| The event schema, and how duplicate events are handled | Order + Credit Services | D3 |
| AWS compute target (ECS Fargate / EKS / other) and IaC tool | Cloud deployment | D3–D4 |
