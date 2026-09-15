# Frontend

The browser client. It is the authority on presentation: screen
organization, navigation, client-side routing and view state, form
ergonomics, and the feedback a user gets while an action is in flight or
after it fails, and on nothing else. Whether an errand may be accepted, a
balance covers one, who may see a record, what a status transition means —
each belongs to the service owning that rule. The frontend renders what the
services return and reports what they refuse.

**The language, framework and build tooling are NOT CHOSEN** (root §2). Assume
no React, Vue, Svelte, Angular, TypeScript, Vite, Next, Tailwind or package
manager. **Ask the owner before writing anything framework-specific**, and do
not settle the question by writing code — that is a human's (root §1). Until
the team records an answer, the stack is open.

## Owner

TBD — one developer owns this folder (root §3). Until it is assigned, treat
stack and screen-flow decisions here as unowned and ask.

## Boundaries

The frontend has no database credentials at all, and needing one means the
design went wrong. Temptations specific to this folder:

- **Re-implementing a rule for instant feedback.** Errand eligibility,
  credit sufficiency, self-acceptance checks, status transitions:
  `order-service` and `credit-service` own these. Client-side validation
  is UX only — required fields, formats, lengths. If the rule can reject
  the action, let the service reject it and render the error.
- **Treating a hidden control as a permission.** Hiding or disabling a
  button is an affordance, not an authorization control; anyone can call
  the API directly. The prototype's "Admin mode" checkbox is exactly this
  trap (see Gotchas).
- **Deriving one service's data from another's.** Need a supplier name on
  an order card? Fetch it from `supplier-service`. Do not dig it out of
  an order payload that does not carry it, and do not ask `order-service`
  to start returning it — adding a field is an interface change, decided
  by that service's owner, spec first (root §8).
- **Editing a backend folder to unblock the UI.** `user-service`,
  `supplier-service`, `order-service`, `credit-service` are other
  people's folders. If the UI needs something the API does not give, stop
  and say so (root §3).
- **Composing across services is fine** — one view may call several
  services. Each call still goes through that service's own client.

## Layout

**Status: a D1 demo artifact exists, but not on this branch and not as a stack
commitment.** A flat static page (`index.html`, `app.js`, `styles.css`) served
by nginx (`nginx.conf`, `Dockerfile`), talking to `supplier-service` only, is
on `origin/feat/supplier-service` (open PR #1); this folder currently holds
only these instruction files. Do not treat that page as the architecture or
grow features onto it without asking. Whatever framework is chosen, these
rules apply:

- **One API client per backend service, in one dedicated directory**,
  generated from that service's `api/openapi.yaml` (root §8). Generated
  code is never hand-edited. No `fetch()` with a hand-written URL in a
  component.
- Until a spec is committed, keep each service's calls in a single
  hand-written module with the same surface, so swapping in the generated
  client is a one-file change.
- Components take data and callbacks, not transport (root §5: data
  coupling is the goal). Shared UI primitives stay inside `frontend/`;
  nothing is shared with a backend service.

## Local development

- **Port 3001**, which `compose.yaml` publishes onto nginx's :80 in the
  prototype (root §3). Backend ports are allocated in that same table —
  8081-8084 — so read them there rather than inventing or assuming one.
- **Env vars:** one base-URL variable per backend service it talks to,
  following the repo's `<SERVICE>_BASE_URL` naming plus whatever prefix the
  chosen framework demands (`VITE_`, `NEXT_PUBLIC_`, …). The prototype
  instead reads a `window.SUPPLIER_API_BASE` global, defaulting to
  `http://localhost:8082`. Every variable goes into the root `.env.example`
  with a placeholder (root §9).
- **Commands:** none exist yet — there is no `Makefile` in the repo at all
  (root §10), and adding a frontend build, lint or test target, a package
  manager or a lockfile is a stack decision, so ask. The prototype runs via
  Compose as the `frontend` service.

## Gotchas

- **Nothing shipped to the browser is secret.** Not a build-time env var,
  not an inlined constant, not a "hidden" field. No `JWT_SECRET`, no
  database URL, no admin key ever appears in this folder.
- **The prototype's "Admin mode" is not auth.** The checkbox makes `app.js`
  send `X-User-Role: ADMIN`, which `supplier-service` currently trusts
  verbatim as an interim measure (its `internal/middleware/auth.go`). Do not
  build real UI on that pattern or present it as access control.
- **Do not assume a shared error envelope.** `supplier-service` today returns
  `{"error": "..."}` with a non-2xx status; whether the others match is an
  interface decision for their owners, not one to standardise from here. Every
  request path still needs loading, empty and failed states, not just the
  success one — M1 names handling invalid and unsuccessful actions explicitly.
- **Responsive is graded, not a nicety.** Every workflow must be usable at
  phone width — forms, modals and tables included. Check at ~375px.
- **Both roles must be complete.** M1 covers requester *and* courier
  workflows; building only the requester side is the easy failure here.
