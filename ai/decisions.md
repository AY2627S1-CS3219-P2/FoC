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

## The picture

[`decisions/architecture.md`](decisions/architecture.md) renders these rows as
Mermaid: trust zones, and the login, request-routing, refresh and revocation
flows. It supersedes the PNG, which is kept as the team's original but is now
out of date on Redis ownership (D-024) and on what logout does to an access
token (D-025a).

## How to add one

Append to the table. Keep the statement short enough that someone can build
against it without asking a follow-up question. If a decision needs more than a
few lines, write it up in `ai/decisions/<id>-<slug>.md` and link it.

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
| D-011 | 2026-09-19 | team | Authentication uses a short-lived **access token (AT)** plus a **refresh token (RT)**. The AT is a JWT carrying `sub` (user ID), `role` (`USER` or `ADMIN`), `jti` (JWT ID) and `exp`. The RT's only purpose is to be exchanged for a new AT | | **Superseded by D-019** (role values only) |
| D-012 | 2026-09-19 | team | `user-service` is the sole issuer of both tokens. It verifies credentials against the User DB, crafts the AT and RT, and stores the RT **hashed** in the User DB | | Accepted |
| D-013 | 2026-09-19 | team | On every subsequent request the gateway verifies the AT's signature, translates the claims into HTTP headers, and forwards the request to the downstream service over gRPC (decided but not built yet) | | Accepted |
| D-014 | 2026-09-19 | team | Revocation is Redis-backed. **Logout:** `user-service` deletes the RT from the User DB and writes the AT's `jti` to a Redis blocklist. **Suspension:** `user-service` writes a `suspended:<uid>` key to Redis, and the gateway rejects any token for that `sub` issued before the suspension timestamp, ending all active sessions | | Accepted |
| D-015 | 2026-09-19 | team | The refresh exchange is routed **through the gateway** (UI → gateway → `user-service`), not direct to `user-service`. The UI therefore holds exactly one base URL | | Accepted |
| D-016 | 2026-09-17 | Nigeltzy (frontend owner) | The frontend is **React + Vite, TypeScript**. Already recorded in root `AGENTS.md` §2 and `frontend/AGENTS.md`; transcribed here so this file is complete | | Accepted |
| D-018 | 2026-09-19 | Nigeltzy | **Provisional** local ports, "whatever works for the time being": API Gateway **8080**, Redis **6379** (its own default). To be revisited before deployment | | Provisional |
| D-019 | 2026-09-19 | Nigeltzy | The `role` claim and the `users.account_role` column both take the values **`STUDENT`** or **`ADMIN`**. Supersedes the `USER`/`ADMIN` wording in D-011; every other part of D-011 stands | | Accepted |
| D-021 | 2026-09-19 | Nigeltzy | **Requester and courier are presentation semantics, not data.** They are not a second role axis, not a column and not a claim — the only stored and asserted roles are D-019's `STUDENT`/`ADMIN`. Closes the open question on a requester/courier toggle | | Accepted |
| D-022 | 2026-09-19 | team (via D-010), stated explicitly by Nigeltzy | **Downstream services do not authenticate the API Gateway.** They accept `X-User-ID` and `X-User-Role` on trust, and that is sound only because of two things together: (a) the gateway **strips** any client-supplied copies of those headers and injects values derived from a JWT whose signature it has verified, so the values cannot be attacker-supplied; and (b) **no service is reachable except through the gateway**, so no attacker can address one directly. Security therefore rests on network isolation — if any service becomes publicly reachable, its access control is void, and it has no way to detect that. Transcribed from the prose in [D-010 detail](decisions/D-010-api-gateway-auth.md) ("only sound while those services are unreachable except through the gateway"), which stated this but never as a citable row | | Accepted — **two caveats.** The strip-and-inject step in (a) is specified only in a `user-service/AGENTS.md` that is on no branch, so it is not yet recorded (§1). D-009 leaves the AWS target undecided, so the "private VPC" it assumes has no decision or code behind it. **Amended by D-030** — one callee does parse the JWT; the strip-and-inject this row rests on is unchanged |
| D-023 | 2026-09-19 | team (Nigeltzy + Zi Yang) | **Access tokens are signed with RS256.** `user-service` holds the private key and is the only process that can mint a token (upholding D-012); the API Gateway holds only the public key and can verify but not issue. Keys are distributed from `user-service`'s `/.well-known/jwks.json` with a `kid`, one active signing key plus retired keys kept until their TTLs lapse. Replaces the symmetric `JWT_SECRET` currently assumed by `api-gateway/internal/config` and both `.env.example` files | | Accepted |
| D-024 | 2026-09-19 | team (Nigeltzy + Zi Yang) | **The API Gateway does not connect to Redis at all.** `user-service` is the only Redis client. On an ordinary request the gateway verifies the RS256 signature and the `exp` claim locally and forwards — no revocation lookup, no call to `user-service`, no extra hop. Removes the §4.1 conflict entirely: exactly one service touches the datastore | | Accepted |
| D-025 | 2026-09-19 | team (Nigeltzy + Zi Yang) | **One known gap is accepted for the prototype phase, deliberately and with the risk understood.** (a) *Revocation latency:* because of D-024 nothing reads the blocklist on the request path, so a logged-out or suspended access token keeps working until `exp` — up to 15 minutes. Logout and suspension bite at **refresh** time, where `user-service` checks the blocklist and `tokens_valid_after`, so a session cannot be extended; only the in-flight AT lingers. It is to be revisited before any non-prototype deployment | | Accepted (prototype only) |
| D-026 | 2026-09-19 | Nigeltzy | **`api-gateway/` is owned by Nigeltzy**, who also owns `frontend/`. Root §3 gives each folder one owner; this records the gateway's | | Accepted |
| D-027 | 2026-09-19 | Nigeltzy (gateway owner) | **The gateway's public route surface.** It *terminates* four auth routes — `POST /auth/register`, `/auth/login`, `/auth/refresh`, `/auth/logout` — and rewrites each to `user-service`'s `/api/v1/users/*` internally. Everything under `/api/<service>/*` is *proxied* by prefix: `suppliers`→:8082, `orders`→:8083, `credits`→:8084, `users`→:8081, after the gateway verifies the token, strips client-supplied claim headers and injects its own (D-022). `GET /healthz` is the gateway's own. The browser's URLs deliberately do not mirror the internal service paths | | Accepted |
| D-028 | 2026-09-19 | Nigeltzy (gateway owner) | **The gateway is written code-first, deviating from D-005's spec-first rule.** Handlers and DTOs are hand-written; `api/openapi.yaml` stays empty and nothing is generated into `internal/gen/`. Taken to get a working barebones gateway sooner, on the explicit understanding that the spec is **backfilled** before the gateway is treated as a stable contract for other services. No other service is exempted | | Accepted (deviation, to be backfilled) |
| D-029 | 2026-09-17 | team | **The frontend's test runner is `vitest`**, chosen alongside the rest of the frontend stack (D-016) and used from the start. Decided on the same day as D-016 and part of the same stack choice. This row only writes it down; it is not a new decision. `frontend/AGENTS.md` wrongly listed a test runner as unchosen until 2026-09-20, which is why it went unrecorded here. It shares Vite's config and transform, so there is no second build pipeline to keep in step. Tests sit beside the code as `*.test.ts`; `npm test` runs them once, `npm run test:watch` watches. Scope today is pure logic only — validation rules and the OTP fixture's timing. **No component testing library is chosen**, so rendering is still unverified, and picking one is a separate stack decision. Recorded in `frontend/AGENTS.md`, whose "not chosen" list this corrects | | Accepted (transcribed 2026-09-20; date confirmed by Nigeltzy) |
| D-030 | 2026-09-21 | Nigeltzy (gateway owner) | **The gateway forwards the access token to `user-service`, and to no other callee.** `/api/users/*` is proxied with `proxy.NewRetainingToken`, which leaves `Authorization` on the outbound request; `/api/suppliers`, `/api/orders` and `/api/credits` keep `proxy.New`, which deletes it, so they never see a bearer token. A deliberate exception to D-022's "downstream services do not authenticate the gateway / do not parse JWTs", for the one service that does: `user-service`'s `RequireJWT` reads the `Authorization` header, and its `api/openapi.yaml` marks `/api/v1/users/{uid}` and `/api/v1/users/{uid}/status` `security: BearerAuth: []`, so under `New` those routes answer 401. **D-022's strip-and-inject is untouched** — the claim-header deletion is still unconditional on every route including this one, and the gateway's own verified `X-User-Id`/`X-User-Role` are still injected. In code since `04475c4` (`api-gateway/internal/proxy/proxy.go`, `internal/httpapi/router.go`) | | Accepted |
| D-031 | 2026-09-21 | team (via `agents_md_changes.md`, applied by GCheeYang) | **Service-to-service calls go over gRPC.** Root `AGENTS.md` §4 rule 3 now reads "Service-to-service calls go over gRPC against the service's generated client, never a hand-rolled connection", and §8 scopes spec-first OpenAPI to "the public contract each service exposes; internal service-to-service calls are gRPC instead". On `main` since `dbeb950` (PR #1, `f779ced`) | | Accepted — **covers the gateway→service hop too** (D-013). **Nothing is built against it yet:** no `grpc` dependency in any `go.mod`, no `.proto` anywhere, and every service on every branch is chi + JSON. D-005's "generated into `internal/gen/`" now covers two pipelines and D-005 itself has not been amended to say so |
| D-032 | 2026-09-21 | team (via `agents_md_changes.md`, applied by GCheeYang) | **The message broker is Kafka.** Added to root `AGENTS.md` §2's stack table. On `main` since `dbeb950` (PR #1, `f779ced`) | | Accepted as written — **nothing built against it.** No broker service in `compose.yaml`, no client in any `go.mod`, and the only trace of an async interaction in the repo is one line in `user-service/AGENTS.md` about publishing an event so Credit Service can allocate initial credits. *Which* interactions are asynchronous, and the event schema and duplicate handling, are still open below |
| D-033 | 2026-09-22 | Nigeltzy (owner of `frontend/` and `api-gateway/`, D-026) | **The access token lives in memory only; the refresh token lives in an `HttpOnly; Secure; SameSite=Strict; Path=/auth` cookie the gateway owns.** The browser is made same-origin with the gateway (Vite `server.proxy` in dev, the gateway serving the built frontend in prod), so **`interimCORS` is deleted rather than configured** and no `credentials: "include"` is needed. `user-service`'s contract does not change: `refreshToken` stays `required` on `LogoutRequest`, and the gateway injects it from the cookie on `/auth/logout` and `/auth/refresh`, sets the cookie from the response on `/auth/login` and `/auth/refresh`, and clears it on logout. The refresh token is never returned to the page. Refresh happens on demand — no access token in memory, or a 401 — not on every page load, and concurrency is handled client-side with `navigator.locks`. Full text, including what it does not change, in [D-033 detail](decisions/D-033-browser-token-storage.md). Closes the Open row "Where the UI keeps the AT and RT" | | Accepted — **two notes.** `Path=/auth` rather than `/auth/refresh` is load-bearing: the narrower scope would stop the browser sending the cookie to `/auth/logout`, whose `LogoutRequest` requires the token, and logout would silently stop revoking. And the point about scoping `revokeCompromisedSessions` to the token family is **`user-service` code, so Zi Yang's** (root §3) — the `sessions` table has a `replaced_by_token_hash` chain but no family id, so a clean scope needs a migration |

## Open — do not implement until these have a row above

Named here so an agent can point at the gap instead of guessing. Each is
graded at D2 or D3, so the answer needs a rationale written alongside it.

**On the draft `user-service/AGENTS.md` Zi Yang circulated on 2026-09-19:** the
team decided on 2026-09-19 to treat it as authoritative and build against it,
even though it is not yet committed to a branch. It answers the header names
(`X-User-ID`, `X-User-Role`), the signing algorithm (now D-023), the token
lifetimes (15 minutes / 7 days) and the auth endpoint paths. Those rows are
struck from the list below on that basis.

**This is a deliberate exception to §1**, which counts a decision as made only
once it is written somewhere the team can point at. It is recorded here so the
exception is visible rather than silent. Pushing `feat/user-service` closes the
gap and costs nothing.

| Question | Blocks | Milestone |
| --- | --- | --- |
| The order state set and which transitions are legal | Order Service | D3 |
| How concurrent acceptance of the same errand is made safe | Order Service | D3 |
| Whether credit reservation is atomic with order persistence | Order + Credit Services | D3 |
| What happens to an errand that stalls after acceptance | Order Service | D3 |
| How credit-operation atomicity is achieved | Credit Service | D3 |
| Which interactions are asynchronous. The broker technology is answered by D-032 (Kafka) | Async workflow (M6) | D3 |
| The event schema, and how duplicate events are handled | Order + Credit Services | D3 |
| AWS compute target (ECS Fargate / EKS / other) and IaC tool | Cloud deployment | D3–D4 |
