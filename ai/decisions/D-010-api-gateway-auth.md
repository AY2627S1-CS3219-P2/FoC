<!--
AI Assistance Disclosure:
Tool: Claude Code (model: Opus 5), date: 2026-09-19
Scope: Transcription of the team's API-gateway architecture diagram and
  token-lifecycle write-up into the repository, so D-010..D-015 in
  ../decisions.md have something to point at. Wording only — no rationale,
  no design choice, and nothing added that the source artifacts did not say.
Author review: PENDING — <reviewer to complete>
-->

# D-010 — API Gateway and the token lifecycle

Detail for rows **D-010 – D-015** in [`../decisions.md`](../decisions.md).

> **Status of this file.** It records *what* the team decided. The **Rationale**
> cells in `decisions.md` are still blank and are the team's to write — root
> `AGENTS.md` §1 forbids an AI tool from authoring them, and D3/D4 grade them
> directly. The source is a team architecture diagram plus a token-lifecycle
> write-up, both produced in discussion on 2026-09-19.
>
> The diagram is committed beside this file:
> [`jwt-token-architecture-diagram.png`](jwt-token-architecture-diagram.png).

## Trust zones

| Zone | Contains |
| --- | --- |
| Publicly accessible | UI Interface, API Gateway |
| Exposed to the API Gateway only | `user-service` auth endpoints |
| Internal only | `user-service` internals, User DB, `supplier-service`, `order-service`, `credit-service` |

Redis sits outside the public zone. It is a separate datastore from the User
DB, holding only revocation state — the `jti` blocklist and `suspended:<uid>`
keys — and never credentials, refresh tokens or profile data.

**D-020 supersedes D-017 on who touches it:** `user-service` is the sole
**writer**, and the API Gateway is a **reader** only. (D-017 had given Redis to
the gateway outright; that is no longer the decision.)

## Token shapes

**Access token (AT)** — a JWT with:

| Claim | Meaning |
| --- | --- |
| `sub` | Subject: the user ID |
| `role` | `STUDENT` or `ADMIN` (D-019) |
| `jti` | JWT ID, the handle used for revocation |
| `exp` | Expiry time |

**Refresh token (RT)** — used only to exchange for a new access token. Stored
**hashed** in the User DB.

## 1. Issuing (login and registration)

1. The UI submits credentials to the API Gateway, which forwards the payload to
   `user-service` over a synchronous REST call.
2. `user-service` authenticates the payload against the User DB and crafts an
   access token (`sub`, `role`, `jti`, `exp`) and a refresh token.
3. The refresh token's hash is stored in the User DB, and both tokens are
   returned through the gateway to the UI.

## 2. Stateless request routing

1. The UI attaches the access token to every subsequent request.
2. The gateway verifies the JWT, checks Redis for revocation of its `jti`,
   translates the claims into HTTP headers, and forwards the request over
   synchronous REST to the downstream service (Order API, Credit API,
   Supplier API, …).

Downstream services do not verify the JWT themselves; they read the headers the
gateway set. This is only sound while those services are unreachable except
through the gateway — the zone table above.

## 3. Refresh flow

1. When the UI detects an expired access token, it sends the refresh token
   **through the gateway** (D-015).
2. `user-service` checks the refresh token against the hash in the User DB and,
   if it is valid, issues a fresh access token.

## 4. Revocation

**Logout.** `user-service` deletes the refresh token from the User DB and writes
the access token's `jti` to a Redis blocklist. The AT stops working before its
`exp`.

**Suspension.** An admin suspends a user; `user-service` writes a
`suspended:<uid>` key to Redis. The gateway then rejects any token whose `sub`
is that user and which was issued before the suspension timestamp, ending every
active session at once.

> Both flows above are the **team diagram's** wording, and D-020 keeps
> `user-service` as the writer, so they stand. `user-service`'s own `AGENTS.md`
> is more specific on two points — the exact key spelling
> (`suspended:uid:<uuid>`) and a mandatory write order (Redis first, then
> PostgreSQL, aborting with a 500 if Redis fails). Those are Zi Yang's to
> record in that folder; this file does not restate them.

## What this does not settle

Listed in the **Open** table of [`../decisions.md`](../decisions.md) rather than
repeated here.

The D-014/D-017 clash that used to block implementation hardest is **resolved
by D-020**: `user-service` writes Redis directly, as D-014 always drew it, so no
gateway-side revocation API is needed. What D-020 leaves open is a written
carve-out against root `AGENTS.md` §4.1 — two services now share one
datastore's connection string, and the team owes that a rationale. (D-003 is
not in play; it covers PostgreSQL specifically.)

Ports are settled only **provisionally** (D-018): gateway 8080, Redis 6379,
chosen to get the stack running rather than decided.
