<!--
AI Assistance Disclosure:
Tool: Claude Code (model: Opus 5), date: 2026-09-19
Scope: Transcription of the team's API-gateway architecture diagram and
  token-lifecycle write-up into the repository, so D-010..D-015 in
  ../decisions.md have something to point at. Wording only — no rationale,
  no design choice, and nothing added that the source artifacts did not say.
Author review: Nigeltzy - The AI was used to format the documentation and update based our team's discussions, which is accurately transcripted.
-->

# D-010 — API Gateway and the token lifecycle

Detail for rows **D-010 – D-015** in [`../decisions.md`](../decisions.md).

> **Status of this file.** It records *what* the team decided. The **Rationale**
> cells in `decisions.md` are still blank and are the team's to write — root
> `AGENTS.md` §1 forbids an AI tool from authoring them, and D3/D4 grade them
> directly. The source is a team architecture diagram plus a token-lifecycle
> write-up, both produced in discussion on 2026-09-19.
>
> The team's original diagram is committed beside this file:
> [`jwt-token-architecture-diagram.png`](jwt-token-architecture-diagram.png).
> **It is now out of date** on Redis ownership (D-024) and on what logout does
> to an access token (D-025a). The current picture is
> [`architecture.md`](architecture.md), drawn in Mermaid so it diffs in review.

## Trust zones

| Zone | Contains |
| --- | --- |
| Publicly accessible | UI Interface, API Gateway |
| Exposed to the API Gateway only | `user-service` auth endpoints |
| Internal only | `user-service` internals, User DB, `supplier-service`, `order-service`, `credit-service` |

Redis sits outside the public zone. It is a separate datastore from the User
DB, holding only revocation state — the `jti` blocklist and `suspended:<uid>`
keys — and never credentials, refresh tokens or profile data.

**D-024:** `user-service` is the **only** process that connects to Redis. The
API Gateway is not a Redis client at all — not a writer, not a reader.

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
   `user-service` over gRPC (D-031; decided but not built yet, so the gateway
   still forwards over HTTP today).
2. `user-service` authenticates the payload against the User DB and crafts an
   access token (`sub`, `role`, `jti`, `exp`) and a refresh token.
3. The refresh token's hash is stored in the User DB, and both tokens are
   returned through the gateway to the UI.

## 2. Stateless request routing

1. The UI attaches the access token to every subsequent request.
2. The gateway verifies the JWT's RS256 signature against the public key
   (D-023) and checks `exp`, then translates the claims into HTTP headers and
   forwards the request to the downstream service (Order API, Credit API,
   Supplier API, …) over gRPC (D-013; decided but not built yet, so the
   gateway still forwards over HTTP today).

   **No revocation lookup happens here (D-024).** The diagram as drawn had the
   gateway checking Redis at this step; it no longer does, and it does not ask
   `user-service` either. Verification is entirely local to the gateway.

Downstream services do not verify the JWT themselves; they read the headers the
gateway set. This is only sound while those services are unreachable except
through the gateway — the zone table above.

**This assumption is now a citable row: D-022.** It rests on two legs, and both
have to hold:

1. **Strip, then inject.** The gateway discards any `X-User-ID` or
   `X-User-Role` the client sent and replaces them with values read from the
   *verified* JWT claims. Without the strip, a client sends
   `X-User-Role: ADMIN` alongside an ordinary token and it passes straight
   through the front door — no network access needed. This step is specified in
   `user-service`'s own `AGENTS.md`, which is on no branch yet, so it is not
   recorded (root `AGENTS.md` §1).
2. **No direct route.** Nothing but the gateway can address a service. In
   `compose.yaml` only the gateway has a `ports:` key, so this holds locally.
   D-009 leaves the AWS target undecided, so it has nothing behind it in a
   deployment yet.

Exposed gateway routes limit *what* an outsider can reach; strip-and-inject
limits *who they are* when they reach it. Neither substitutes for the other.

## 3. Refresh flow

1. When the UI detects an expired access token, it sends the refresh token
   **through the gateway** (D-015).
2. `user-service` checks the refresh token against the hash in the User DB and,
   if it is valid, issues a fresh access token.

## 4. Revocation

**Logout.** `user-service` deletes the refresh token from the User DB and writes
the access token's `jti` to a Redis blocklist.

> The diagram says the AT "stops working before its `exp`". Under **D-024** it
> does not — nothing on the request path reads that blocklist. The AT remains
> usable until `exp`; what the blocklist stops is the *refresh*. See D-025.

**Suspension.** An admin suspends a user; `user-service` writes a
`suspended:<uid>` key to Redis. The gateway then rejects any token whose `sub`
is that user and which was issued before the suspension timestamp, ending every
active session at once.

> Both flows above are the **team diagram's** wording, and D-024 keeps
> `user-service` as the writer, so they stand. `user-service`'s own `AGENTS.md`
> is more specific on two points — the exact key spelling
> (`suspended:uid:<uuid>`) and a mandatory write order (Redis first, then
> PostgreSQL, aborting with a 500 if Redis fails). Those are Zi Yang's to
> record in that folder; this file does not restate them.

## What this does not settle

Listed in the **Open** table of [`../decisions.md`](../decisions.md) rather than
repeated here.

D-024 takes the gateway out of Redis altogether, so exactly one service touches
the datastore and root `AGENTS.md` §4.1 holds without a carve-out.

**What D-024 costs, recorded as D-025:** nothing reads the blocklist on the
request path any more, so a logged-out or suspended access token keeps working
until it expires — up to 15 minutes. Logout and suspension take effect at
**refresh** time, where `user-service` checks the blocklist and
`tokens_valid_after` and refuses to extend the session. The blocklist's job is
therefore to stop a session being renewed, not to kill an access token
mid-flight. Accepted for the prototype phase; to be revisited before any real
deployment.

Ports are settled only **provisionally** (D-018): gateway 8080, Redis 6379,
chosen to get the stack running rather than decided.
