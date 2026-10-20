# e2e

The whole-stack test suite. The tests drive the Compose stack through the API
Gateway, the way the browser does: real accounts, real tokens, and only the
gateway's public paths. This is its own Go module (`foc/e2e`) and imports
nothing from the services. Every Go file except `doc.go` is behind the `e2e`
build tag, so `go test ./...` without the tag runs nothing.

## Run it

First-time setup is the same as for any `docker compose up`: a filled root
`.env` (copy `.env.example`; root `AGENTS.md` §11 has the admin values) and
`user-service/keys.json` (see `user-service/README.md`). Keep
`OTP_DELIVERY=log`, the `.env.example` value and Compose's default: the suite
reads each new student's verification code from user-service's log.

Then, from the repository root, one command starts the stack with T and C in
seconds and runs the suite:

```bash
ORDER_EXPIRY_PERIOD=20s ORDER_CONFIRMATION_WINDOW=20s docker compose up --build --wait && go -C e2e test -tags=e2e -count=1 ./...
```

`--wait` returns once every container is up, and stops the command if one fails
to start. `-count=1` stops Go from replaying a cached pass against a stack that
may have changed. Most of the run is spent waiting out T and C, which is why the
stack runs with them in seconds; the tests run in parallel, so the waits
overlap.

The stack keeps those timers afterwards. To put the default timers back,
recreate order-service without them: `docker compose up -d order-service`.

To run one test against a stack that is already up:

```bash
go -C e2e test -tags=e2e -count=1 -run TestFullFlowPaysTheCourier ./...
```

## Settings

| Variable | Default | Meaning |
| --- | --- | --- |
| `E2E_GATEWAY_URL` | `http://localhost:8080` | The gateway the suite calls |
| `E2E_ADMIN_USERNAME` | `admindemo` | An ADMIN account (root `AGENTS.md` §11) |
| `E2E_ADMIN_PASSWORD` | `AdminDemo123` | Its password |
| `E2E_EXPIRY_PERIOD` | `20s` | The `ORDER_EXPIRY_PERIOD` (T) the stack runs with |
| `E2E_CONFIRMATION_WINDOW` | `20s` | The `ORDER_CONFIRMATION_WINDOW` (C) the stack runs with |

The two timer settings only tell the suite what to expect. The expiry and
auto-confirm tests read T and C off the errands they create, and fail at once,
naming both values, when the stack runs with something else. K is taken to be 2,
Compose's default; the capacity test fails if the stack runs with another
`ORDER_MAX_ACTIVE_ASSIGNMENTS`.

## Preflight

Before any test runs, `TestMain` checks that the stack has every piece the suite
drives:

| Check | Request | Needs |
| --- | --- | --- |
| Gateway | `GET /healthz` answers 200 | api-gateway |
| Accounts | `POST /auth/register`, the code from `docker compose logs user-service`, `POST /auth/verify-otp`, then `POST /auth/login` | user-service with `OTP_DELIVERY=log`, and the `docker` CLI |
| Errands | `GET /api/orders/locations` answers 200 | order-service, and the gateway keeping `Authorization` on `/api/orders` |
| Credits | `GET /api/credits/accounts/me` answers 200 within 15 s of verifying the email | credit-service, user-service's `user.registered` producer, RabbitMQ and the gateway's `/api/credits` change |

If anything is missing it stops with one message that lists what is missing,
instead of every test timing out on its own.

## What it covers

| Test | What it checks |
| --- | --- |
| `TestFullFlowPaysTheCourier` | create, accept, pickup, deliver, confirm; the reward is held on create and paid to the courier on completion |
| `TestRequesterCancelRefundsTheReward` | the requester cancels an `OPEN` errand and gets the hold back |
| `TestExpiredErrandRefundsTheReward` | after T, accept is refused before the sweeper runs, the errand becomes `EXPIRED`, and the hold comes back |
| `TestConcurrentAcceptsHaveOneWinner` | ten couriers accept one errand at once: one 200, nine 409, one courier assigned |
| `TestCourierAtCapacityCannotAccept` | a courier holding K errands gets 409 on the next accept, and can accept again once one is delivered |
| `TestRequesterCannotAcceptOwnErrand` | 403, and the errand stays `OPEN` |
| `TestWithdrawnErrandCanBeAcceptedAgain` | withdraw puts the errand back to `OPEN` with a fresh deadline, the courier who withdrew gets 409 and no accept action, a second courier takes it, and the first keeps a `RELEASED` record |
| `TestUnconfirmedDeliveryAutoConfirms` | a delivery nobody confirms completes after C and pays the courier |
| `TestAdminCancelsAcceptedErrand` | the requester cannot cancel once accepted; an ADMIN can, refunding the requester and paying the courier nothing |
| `TestBrowserCannotWriteCredits` | `POST /api/credits/reservations` and `PATCH /api/credits/reservations/{errandId}` answer 404 or 405, and no credits move |
| `TestRewardChangeMovesTheHold` | raising and then lowering a reward moves the hold each time, and cancelling returns all of it |
| `TestLogoutRevokesTheAccessToken` | a student's token works on `GET /api/orders/open`, and after `POST /auth/logout` the same token gets 401 |
| `TestSuspensionRevokesTheAccessToken` | after an ADMIN suspends a student with `PATCH /api/users/{uid}/status`, the token the student already holds gets 401 on `GET /api/orders/open` |
| `TestGatewayRefusesALoggedOutToken` | after `POST /auth/logout`, the same token gets 401 on `GET /api/suppliers` and `GET /api/credits/accounts/me`, routes whose service does not check the revocation keys itself |
| `TestGatewayRefusesASuspendedTokenUntilReactivated` | after an ADMIN suspends a student, their token gets 401 on `GET /api/suppliers`, `GET /api/credits/accounts/me` and `GET /api/users/{uid}`; after reactivation a fresh login gets 200 while the old token stays refused |
| `TestSuspendedStudentIsRefusedAtOnce` | a suspended student's existing token gets 401 on `GET /api/users/{uid}`, and a newly issued token works after reactivation |
| `TestLoggedOutTokenIsRefused` | after logout, the same token gets 401 on `GET /api/users/{uid}` |
| `TestSuspendedAccountGetsNoNewCode` | after an ADMIN suspends a student, `POST /auth/resend-otp` answers 409 and user-service logs no additional OTP for that email |
| `TestAdminManagesASupplier` | an ADMIN creates a supplier (201), reads it back as sent, updates it (200), deletes it (204); then `GET` is 404, `GET /api/suppliers?q=` no longer lists it, and `GET /api/suppliers/not-a-uuid` is 404 |
| `TestACreateThatLeavesOutIsAvailableIsUnavailable` | a `POST` with no `is_available` makes an unavailable supplier (D-072); the suite's other supplier bodies always send it |
| `TestStudentCannotWriteSuppliers` | a student's create, update and delete are each 403, also with `X-User-Role: ADMIN` and `X-User-Id` forged; the admin's read shows the supplier unchanged |
| `TestSupplierWithUnfinishedErrandIsKept` | with an unfinished errand naming the supplier, `DELETE` is 409 and the supplier is still readable; after the requester cancels, `DELETE` is 204 and the errand still carries the supplier's name (D-052) |
| `TestLoggedOutAdminCannotWriteSuppliers` | after `POST /auth/logout`, the admin's access token, not yet expired, gets 401 on `POST /api/suppliers`, and the write created nothing |
<!-- AI Assistance Disclosure: Codex (GPT-5), date: 2026-10-19 — documents issue #32's user-service route checks. -->
<!-- AI Assistance Disclosure: Codex (GPT-5), date: 2026-10-19 — documents the issue #31 E2E coverage. -->

## Notes

- Every test registers its own students (`e2e<random>@u.nus.edu`) and runs in
  parallel with the others; no test reads another's data or depends on order.
- The supplier tests make their own suppliers, named `e2e-<random hex>` with a
  random location, and delete them as an admin when they finish (a 404 there is
  fine). `availableSupplier` skips names starting `e2e-`, so an errand test never
  picks a supplier one of them is about to delete. A failed run can leave one
  behind; it never matches a later run's, and `docker compose down -v` wipes it.
- Those accounts and errands stay in your local databases. Errands left
  `ACCEPTED` or `PICKED_UP` have no timer, so they stay active.
  `docker compose down -v` wipes everything.
- Registering needs the emailed code. The register helper in `client_test.go`
  reads it with `docker compose logs user-service`, run in the repository root,
  so it finds the stack that `docker compose up` started there.
