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
     Wording only — no Rationale cell filled, no row invented.

     Rows D-019..D-021 and the Superseded marks on D-011/D-017: AI-transcribed
     (Claude Code, Opus 5, 2026-09-19) from Nigeltzy's decisions in session,
     taken against Zi Yang's user-service AGENTS.md. Wording only — the
     Rationale cells are deliberately blank and are the team's to write.

     Row D-022: AI-transcribed (same session) from the existing prose in
     ai/decisions/D-010-api-gateway-auth.md, which already stated the trust
     assumption but not as a citable row. No new decision — it makes an
     implicit one explicit so the team can point at it. -->
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
| D-013 | 2026-09-19 | team | On every subsequent request the gateway verifies the AT's signature, checks Redis for revocation, translates the claims into HTTP headers, and forwards the request to the downstream service over synchronous REST | | **Amended by D-024** — the gateway no longer checks Redis; the rest stands |
| D-014 | 2026-09-19 | team | Revocation is Redis-backed. **Logout:** `user-service` deletes the RT from the User DB and writes the AT's `jti` to a Redis blocklist. **Suspension:** `user-service` writes a `suspended:<uid>` key to Redis, and the gateway rejects any token for that `sub` issued before the suspension timestamp, ending all active sessions | | Accepted |
| D-015 | 2026-09-19 | team | The refresh exchange is routed **through the gateway** (UI → gateway → `user-service`), not direct to `user-service`. The UI therefore holds exactly one base URL | | Accepted |
| D-016 | 2026-09-17 | Nigeltzy (frontend owner) | The frontend is **React + Vite, TypeScript**. Already recorded in root `AGENTS.md` §2 and `frontend/AGENTS.md`; transcribed here so this file is complete | | Accepted |
| D-017 | 2026-09-19 | team | **The API Gateway owns Redis.** It is a separate datastore from the User DB — different engine, different process, different connection string — and holds only revocation state (the `jti` blocklist and `suspended:<uid>` keys), never credentials, refresh tokens or profile data. This resolves the D-013/D-014 clash with root `AGENTS.md` §4.1 and D-003 | | **Superseded by D-020** |
| D-018 | 2026-09-19 | Nigeltzy | **Provisional** local ports, "whatever works for the time being": API Gateway **8080**, Redis **6379** (its own default). To be revisited before deployment | | Provisional |
| D-019 | 2026-09-19 | Nigeltzy | The `role` claim and the `users.account_role` column both take the values **`STUDENT`** or **`ADMIN`**. Supersedes the `USER`/`ADMIN` wording in D-011; every other part of D-011 stands | | Accepted |
| D-020 | 2026-09-19 | Nigeltzy | **`user-service` is the sole writer to the Redis blocklist; the API Gateway is a reader only.** Supersedes D-017, which gave Redis to the gateway. This restores the D-014 model: on logout `user-service` pushes the AT's `jti` to Redis and then revokes the RT in PostgreSQL, and on suspension it writes `suspended:uid:<uuid>` and then sets `tokens_valid_after`. The gateway reads the blocklist to decide whether to reject, and rejects the request outright if it cannot reach Redis | | **Superseded by D-024.** The §4.1 carve-out it owed is no longer needed: under D-024 only `user-service` touches Redis |
| D-021 | 2026-09-19 | Nigeltzy | **Requester and courier are presentation semantics, not data.** They are not a second role axis, not a column and not a claim — the only stored and asserted roles are D-019's `STUDENT`/`ADMIN`. Closes the open question on a requester/courier toggle | | Accepted |
| D-022 | 2026-09-19 | team (via D-010), stated explicitly by Nigeltzy | **Downstream services do not authenticate the API Gateway.** They accept `X-User-ID` and `X-User-Role` on trust, and that is sound only because of two things together: (a) the gateway **strips** any client-supplied copies of those headers and injects values derived from a JWT whose signature it has verified, so the values cannot be attacker-supplied; and (b) **no service is reachable except through the gateway**, so no attacker can address one directly. Security therefore rests on network isolation — if any service becomes publicly reachable, its access control is void, and it has no way to detect that. Transcribed from the prose in [D-010 detail](decisions/D-010-api-gateway-auth.md) ("only sound while those services are unreachable except through the gateway"), which stated this but never as a citable row | | Accepted — **two caveats.** The strip-and-inject step in (a) is specified only in a `user-service/AGENTS.md` that is on no branch, so it is not yet recorded (§1). The isolation in (b) is not true today: `compose.yaml` publishes `supplier-service` on `0.0.0.0:8082`, and D-009 leaves the AWS target undecided, so the "private VPC" it assumes has no decision or code behind it |
| D-023 | 2026-09-19 | team (Nigeltzy + Zi Yang) | **Access tokens are signed with RS256.** `user-service` holds the private key and is the only process that can mint a token (upholding D-012); the API Gateway holds only the public key and can verify but not issue. Keys are distributed from `user-service`'s `/.well-known/jwks.json` with a `kid`, one active signing key plus retired keys kept until their TTLs lapse. Replaces the symmetric `JWT_SECRET` currently assumed by `api-gateway/internal/config` and both `.env.example` files | | Accepted |
| D-024 | 2026-09-19 | team (Nigeltzy + Zi Yang) | **The API Gateway does not connect to Redis at all.** `user-service` is the only Redis client. On an ordinary request the gateway verifies the RS256 signature and the `exp` claim locally and forwards — no revocation lookup, no call to `user-service`, no extra hop. Supersedes D-020 (gateway as reader) and amends D-013 (which had the gateway checking Redis per request). Removes the §4.1 conflict entirely: exactly one service touches the datastore | | Accepted |
| D-025 | 2026-09-19 | team (Nigeltzy + Zi Yang) | **Two known gaps are accepted for the prototype phase, deliberately and with the risk understood.** (a) *Revocation latency:* because of D-024 nothing reads the blocklist on the request path, so a logged-out or suspended access token keeps working until `exp` — up to 15 minutes. Logout and suspension bite at **refresh** time, where `user-service` checks the blocklist and `tokens_valid_after`, so a session cannot be extended; only the in-flight AT lingers. (b) *Network isolation:* `compose.yaml` still publishes `supplier-service` on `0.0.0.0:8082`, so D-022's second leg does not hold locally. Both are to be revisited before any non-prototype deployment | | Accepted (prototype only) |
| D-026 | 2026-09-19 | Nigeltzy | **`api-gateway/` is owned by Nigeltzy**, who also owns `frontend/`. Root §3 gives each folder one owner; this records the gateway's | | Accepted |
| D-027 | 2026-09-19 | Nigeltzy (gateway owner) | **The gateway's public route surface.** It *terminates* four auth routes — `POST /auth/register`, `/auth/login`, `/auth/refresh`, `/auth/logout` — and rewrites each to `user-service`'s `/api/v1/users/*` internally. Everything under `/api/<service>/*` is *proxied* by prefix: `suppliers`→:8082, `orders`→:8083, `credits`→:8084, `users`→:8081, after the gateway verifies the token, strips client-supplied claim headers and injects its own (D-022). `GET /healthz` is the gateway's own. The browser's URLs deliberately do not mirror the internal service paths | | Accepted |
| D-028 | 2026-09-19 | Nigeltzy (gateway owner) | **The gateway is written code-first, deviating from D-005's spec-first rule.** Handlers and DTOs are hand-written; `api/openapi.yaml` stays empty and nothing is generated into `internal/gen/`. Taken to get a working barebones gateway sooner, on the explicit understanding that the spec is **backfilled** before the gateway is treated as a stable contract for other services. No other service is exempted | | Accepted (deviation, to be backfilled) |

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
| Whether the gateway is a Go service we write or an off-the-shelf product | API Gateway | D2 |
| Where the UI keeps the AT and RT | `frontend/` | D2 |
| The order state set and which transitions are legal | Order Service | D3 |
| How concurrent acceptance of the same errand is made safe | Order Service | D3 |
| Whether credit reservation is atomic with order persistence | Order + Credit Services | D3 |
| What happens to an errand that stalls after acceptance | Order Service | D3 |
| How credit-operation atomicity is achieved | Credit Service | D3 |
| Which interactions are asynchronous, and the broker technology | Async workflow (M6) | D3 |
| The event schema, and how duplicate events are handled | Order + Credit Services | D3 |
| AWS compute target (ECS Fargate / EKS / other) and IaC tool | Cloud deployment | D3–D4 |
