<!-- AI Assistance Disclosure:
Tool: Claude Code (model: Sonnet 5.5), date: 2026-11-03
Scope: The shared shapes the admin dashboard decisions D-104 to D-123 leave open:
  the audit table's columns, the action codes, and each service's admin endpoints.
  Proposed by Claude Code at the owner's request, for the owner to approve or edit.
Author review: PENDING — <reviewer to complete> -->

# Admin dashboard: the shapes D-104 to D-123 leave open

**Status: accepted (D-124), owner approved 2026-11-03.** Everything here follows from the owner's decisions
D-104 to D-123 in `ai/decisions.md`. Where a decision left a name, a type or a
route open, this file fills it in so the three services can be built to the same
shape. Edit this file to change one, and the code follows.

## 1. The audit record (D-107, D-113)

One table, `admin_audit`, in each of user-service, order-service and
supplier-service, with the same columns, so the dashboard merges the three by
`occurred_at`.

| Column | Type | Holds |
| --- | --- | --- |
| `id` | uuid, generated | the row |
| `occurred_at` | timestamptz | when it happened (server time) |
| `actor_id` | uuid | the admin, from the verified token's `sub` (D-041), never a header |
| `actor_role` | text | the token's role claim, always `ADMIN` today |
| `action` | text | the code below |
| `target_type` | text | `supplier`, `account` or `errand` |
| `target_id` | text | the id acted on; empty when a create was refused before it got one |
| `outcome` | text | `succeeded`, `refused` or `failed` (D-109) |
| `refusal_reason` | text | why it was refused or failed; empty on success |
| `reason` | text | the admin's own reason (D-116); empty unless one is taken |
| `before_state`, `after_state` | jsonb | a status change, e.g. `{"account_status":"ACTIVE"}`; null otherwise |
| `additional_comments` | jsonb | supplier: the new value of every edited field (D-112); account: the names of the fields a profile edit changed, never their values (D-127); null for a status change |
| `correlation_id` | text | the request id, to tie the row to the access log |

Indexes: `occurred_at DESC`, and `(target_type, target_id)`. No other index (D-105).

**Immutable in the database (D-114, D-115).** A trigger refuses every `UPDATE`
and refuses a `DELETE` of a row under 6 months old. The scheduled purge, which
runs at start and every 24 hours, deletes the older ones; nothing else can.

**Known limit (D-129).** The trigger stops the application and ordinary admins, but
not the table's owner, who can disable it. Each service connects as that owner today.
Splitting it into a migration role and a restricted runtime role is left to the AWS
database setup (D-009).

**Written with the action (D-108).** The action's own write and its `succeeded`
row share one database transaction. A refused or failed attempt rolls that
transaction back, so its row is a separate write afterwards (D-109); if that
write fails too, the attempt is logged to stderr and the caller still gets the
real error. Only an authenticated admin's attempts are logged; a student's 403
and a missing token are not admin actions.

## 2. Action codes

| Code | Service | target_type | What it logs |
| --- | --- | --- | --- |
| `supplier.create` | supplier-service | supplier | `additional_comments`: the fields it set |
| `supplier.update` | supplier-service | supplier | `additional_comments`: new value of each changed field |
| `supplier.delete` | supplier-service | supplier | `additional_comments`: the name and building |
| `account.suspend` | user-service | account | `before_state` / `after_state`: `account_status` |
| `account.reactivate` | user-service | account | `before_state` / `after_state`: `account_status` |
| `account.password_reset` | user-service | account | nothing: no value, only that it happened (D-111) |
| `errand.cancel` | order-service | errand | `before_state` / `after_state`: `state` |
| `errand.complete` | order-service | errand | `before_state` / `after_state`: `state` |

## 3. Reading the log (D-104, D-110)

Each service serves its own log to any admin (D-110), through the gateway prefix
it already has:

| Service | Route | Through the gateway |
| --- | --- | --- |
| supplier-service | `GET /suppliers/audit-log` | `/api/suppliers/audit-log` |
| user-service | `GET /api/v1/users/audit-log` | `/api/users/audit-log` |
| order-service | `GET /audit-log` | `/api/orders/audit-log` |

Query: `action`, `outcome`, `actor_id`, `target_id`, `from` and `to` (RFC 3339 or
`YYYY-MM-DD`; `to` is exclusive), `limit` (at most 50, the default) and `offset`.
Newest first. Answer:
`{"items":[{id, occurred_at, actor_id, actor_role, action, target_type, target_id,
outcome, refusal_reason, reason, before_state, after_state, additional_comments,
correlation_id}], "total": n, "limit": 50, "offset": 0}`. A bad query value is 400;
no token is 401; a student is 403.

## 4. Accounts (user-service, D-118 to D-123)

- **Search and list:** `GET /api/v1/users` (admin). `q` matches anywhere in the
  username or email, ignoring case (D-123); `status` is `ACTIVE`, `SUSPENDED` or
  `UNVERIFIED` (D-121); `limit` (at most 50) and `offset`. Sorted by username.
  Each item: `uid, username, email, account_status, account_role, date_created,
  last_login_date, phone_num` (D-122). The page shape is the one above.
- **Suspending:** `PATCH /api/v1/users/{uid}/status` refuses a target that is an
  ADMIN (D-118) and a target that is the caller (D-119) with **403**, and both
  refusals are logged (D-109). The last-active-admin guard stays as a backstop.
- **Reinstating** is unchanged (D-120).
- **No reason is taken** on a suspension: that is James's decision (D-116). The
  audit row's `reason` stays empty until he decides.

## 5. Errands (order-service, D-105, D-117)

- **Past errands:** the admin list `GET /` takes `scope=history` as well as
  `active`. History lists COMPLETED, CANCELLED and EXPIRED errands, filtered by
  `state`, `from` and `to` (on when the errand reached its final state) and
  `supplier_id`, sorted by `sort=recent` (the default) or `sort=supplier_name`
  (A to Z), paged by `limit` (at most 50) and `offset`. The answer is the errand
  list plus `total`, `limit` and `offset`.
- **By user (D-117):** `scope=active` and `scope=history` both accept `user_id`,
  which matches errands where that user is the requester or the courier.
- **Counts:** `GET /users/{userId}/errand-counts` (admin) returns, for the user
  as requester and as courier, how many errands are in each state.
- **Admin actions** (`errand.cancel`, `errand.complete`) log as in §2.

## 6. The account review (D-117)

Built in the frontend from four calls: `GET /api/users/{uid}` (profile and last
login), `GET /api/orders?scope=active|history&user_id={uid}` (the errands),
`GET /api/orders/users/{uid}/errand-counts` (the counts) and
`GET /api/users/audit-log?target_id={uid}` (audit entries about the account).
"About an account" means user-service's entries whose target is that account;
an admin's cancel of the user's errand is an errand entry, not an account one.
