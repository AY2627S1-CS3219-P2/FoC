<!--
AI Assistance Disclosure:
Tool: Claude Code (model: Opus 5), date: 2026-09-22
Scope: TRANSCRIPTION of Nigeltzy's decision, taken to the team. The fifteen
  numbered points are his, restated; the "what this changed in the code"
  section records what was built against them. No Rationale is offered here
  and none was written — the Why column in ../decisions.md is his to fill.
Author review: Nigeltzy - The AI was used to help summarise and format my response so that the team can read my rationale. 
I had decided on the architecture before telling the AI, the decision was made based on my prerequisite knowledge of CS
prior to this, and some of my own research.
-->

# D-033 — Where the browser keeps the access and refresh tokens

Closes the Open row "Where the UI keeps the AT and RT" (`frontend/`, D2).

Decided by **Nigeltzy** (owner of `frontend/` and `api-gateway/`, D-026) on
2026-09-22, after taking the options to the team.

## The shape

- **Access token: in memory only.** Never serialised to `localStorage`,
  `sessionStorage`, a cookie, or anything else. The rule is "in memory, never
  written to storage" — *not* "a module-level variable", which root
  `AGENTS.md` §5 rules out. `lib/tokens.ts`'s existing closure already
  satisfies it and does not change.
- **Refresh token: an `HttpOnly` cookie the gateway owns.**
  `HttpOnly; Secure; SameSite=Strict; Path=/auth; Max-Age=<RT TTL>`.
- **Refresh on demand**, not on every page load: when an authenticated
  request finds no access token in memory, or on a 401.

## Same-origin, not an allowlist

The browser talks to one origin. **`interimCORS` is deleted, not configured.**

- **Dev:** Vite's `server.proxy` routes `/api/*` and `/auth/*` to `:8080`.
  The browser only ever addresses `:3001`.
- **Prod:** the gateway serves the built frontend from `/`. nginx is to be
  added only if that proves unworkable, and flagged before it is.
- No `Access-Control-Allow-Origin`, no `Access-Control-Allow-Credentials`, no
  allowlist, no preflight path anywhere.
- No `credentials: "include"` — same-origin requests send cookies by default,
  so the `fetch` options are left alone.
- The gateway's port is not exposed to the browser in production.

`SameSite=Strict` was never the blocker: `:3001` and `:8080` are same-*site*,
because SameSite ignores ports. CORS was the blocker. It is not to be loosened.

## user-service is untouched

- **No contract change.** `api/openapi.yaml` stays as committed, and
  `refreshToken` remains `required` on `LogoutRequest`.
- **The gateway owns the refresh token.** It reads the cookie and injects
  `refreshToken` into the body for `/auth/logout` and `/auth/refresh`, sets
  the cookie from user-service's response, and clears it on logout.
- **The refresh token is never returned to the frontend** in a response body.
- **`Path=/auth`, not `/auth/refresh`.** Scoping it to the refresh endpoint
  alone would stop the browser sending it to `/auth/logout`, and that is the
  call that revokes the session.

**Login was added to this list after the fact.** The original numbering named
only `/auth/logout` and `/auth/refresh`. `/api/v1/users/login` returns an
`AuthResponse` carrying the refresh token, so it is where the cookie is first
set and where the token must first be stripped from the body.
`/api/v1/users/register` returns `201` with no body and needs nothing.

## Concurrency

- Handled **client-side** with `navigator.locks`, so two tabs cannot present
  the same refresh token at once.
- **No server-side rotation leeway window.** The suspended-tab case — a tab
  waking with a token that has since been rotated away — is an accepted
  limitation.
- `revokeCompromisedSessions` should scope to the token family **if that is a
  small change**, and stay uid-wide if it needs a migration. **This is
  `user-service` code and therefore Zi Yang's** (root §3). Finding passed to
  him: the `sessions` table has `jti`, `uid`, `token_hash` and
  `replaced_by_token_hash` — a rotation chain, but no family or lineage id, so
  a clean family scope needs a migration.

## No change for localhost

`Secure` cookies are accepted on `http://localhost` by current browsers, so
local development is unaffected. This does not extend to a LAN IP.

## What this changed in the code

| Where | What |
| --- | --- |
| `api-gateway/internal/proxy/session_cookie.go` | New. Cookie attributes, and the JSON body translation both ways |
| `api-gateway/internal/proxy/proxy.go` | `NewSessionIssuing` / `NewSessionRotating` / `NewSessionEnding`; cookie→body in `Rewrite`, body→cookie in `ModifyResponse` |
| `api-gateway/internal/httpapi/router.go` | `interimCORS` deleted; one constructor per auth route |
| `api-gateway/internal/config` | `REFRESH_TOKEN_TTL`, the cookie's `Max-Age` |
| `frontend/` | The store holds only the access token; `navigator.locks` around the refresh; Vite dev proxy |

## What this does NOT change

**Revocation still lags.** D-025a is untouched: nothing on the request path
reads the blocklist, so an access token outlives a logout by up to its `exp`.
Moving the refresh token into a cookie makes it harder to steal; it does not
make a stolen access token die any sooner.
