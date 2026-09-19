# Frontend

The browser client. It is the authority on presentation: screen
organization, navigation, client-side routing and view state, form
ergonomics, and the feedback a user gets while an action is in flight or
after it fails, and on nothing else. Whether an errand may be accepted, a
balance covers one, who may see a record, what a status transition means —
each belongs to the service owning that rule. The frontend renders what the
services return and reports what they refuse.

**Stack: React + Vite, in TypeScript.** Recorded by Nigeltzy, the folder owner,
on 2026-09-17; root §2's table carries the same row. This is the record root §1
asks for — an agent may now write React/Vite/TS code in this folder.

**What is still NOT chosen**, and still needs the owner before any code assumes
it: state management beyond React's own hooks, a router, a component or CSS
framework (no Tailwind, no MUI), a test runner, and a linter. Assume none of
them exist. Adding one is a stack decision, not an implementation detail —
ask, and record the answer here before building on it (root §1).

## Owner

Nigeltzy (root §3). Stack and screen-flow decisions here are theirs; an agent
implements one they have recorded and stops (root §1).

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

Feature-sliced. Each feature folder maps to one backend service, so `features/`
grows alongside the services in the repo root.

```
src/
├── features/
│   ├── auth/          LoginPage + session types      [MOCK]
│   ├── home/          landing view
│   ├── suppliers/     view + components + suppliersApi.ts + types.ts
│   ├── errands/       NewErrandView, MyErrandsView    [MOCK]
│   ├── credits/       CreditsView                     [MOCK]
│   └── profile/       ProfileView                     [MOCK]
├── components/        shared only — AppShell, Modal, Toast, MockBadge, icons
├── lib/               config.ts (env), http.ts (transport), mock.ts (fixtures)
├── App.tsx            session gate, view switching, acting mode, balance
├── views.ts           view names + the nav model, until a router is chosen
└── main.tsx
```

Every feature folder maps to one backend service. `suppliers/` is the only one
with a real service behind it.

`index.html` is the Vite entry document; the D1 prototype it replaced
(`app.js` plus hand-written markup) is in this branch's history. The
`Dockerfile` builds with Node and serves the output from nginx via
`nginx.conf`. These rules apply:

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

## Mock services — read before touching them

`user-service`, `order-service` and `credit-service` do not exist. So the
frontend can be built and demoed, `auth/`, `errands/`, `credits/` and
`profile/` talk to fixture modules in their own folders instead
(`src/lib/mock.ts` has the full note).

**Those shapes are not a contract.** They were invented here to have something
to render. An API interface belongs to its service's owner (root §1), and a
change reaching into another service needs that owner (root §3). So:

- Nobody may treat a fixture's shape as the spec for their service.
- When a real service lands, its owner's `api/openapi.yaml` wins and the
  fixture module is **replaced**, not reconciled. Because components only ever
  go through the feature's api module, that is a one-file change.
- Every screen backed by a fixture renders `<MockBadge />`, so a graded demo
  cannot show invented figures as live data. Do not remove those badges while
  the data is still fixture data.
- Do not grow a fixture into a rule. Eligibility, expiry, credit sufficiency
  and state transitions belong to the services that own them; the fixtures
  store and return, nothing more.

**One exception, which is not a mock.** `ErrandStatus` in
`features/errands/types.ts` transcribes the order-state set the team has
already recorded in the milestone report glossary — OPEN, ACCEPTED, PICKED_UP, DELIVERED,
COMPLETED, CANCELLED, EXPIRED, with CANCELLED and EXPIRED terminal. That is a
recorded decision under root §1, so it is implemented rather than invented.
Which transitions are permitted (F3.8) stays with order-service and is never
enforced in this folder. Only the wire casing is still open.

Two hard-coded lists are placeholders nobody owns yet, flagged for the team:
campus delivery locations (`NewErrandView`) and the 100-credit initial
allocation quoted on the login screen.

## Local development

- **Port 3001**, which `compose.yaml` publishes onto nginx's :80 in the
  prototype (root §3). Backend ports are allocated in that same table —
  8081-8084 — so read them there rather than inventing or assuming one.
- **Env vars:** one base-URL variable per backend service it talks to,
  following the repo's `<SERVICE>_BASE_URL` naming plus whatever prefix the
  chosen framework demands — here `VITE_`. Today that is
  `VITE_SUPPLIER_BASE_URL`, resolved in `src/lib/config.ts` and defaulting to
  `http://localhost:8082`. Every variable goes into the root `.env.example`
  with a placeholder (root §9) and this folder's own.
- **Commands:** `npm install`, then `npm run dev` (Vite on 3001),
  `npm run build` (typecheck + production build) and `npm run preview`. There
  is still no `Makefile` in the repo (root §10) and no lint or test target —
  adding either means choosing a linter or test runner, which is a stack
  decision, so ask. Under Compose this folder is the `frontend` service.

## Gotchas

- **Nothing shipped to the browser is secret.** Not a build-time env var,
  not an inlined constant, not a "hidden" field. No `JWT_SECRET`, no
  database URL, no admin key ever appears in this folder.
- **The "Admin mode" checkbox is not auth.** It makes `suppliersApi.ts`
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
