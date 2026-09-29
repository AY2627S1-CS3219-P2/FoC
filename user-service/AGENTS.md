# User Service

The identity authority for the platform (mandatory requirement **M2**):
accounts, credentials, sessions/tokens, profile information, and the role a
person acts in — requester, courier, or admin — including the toggle between
requester and courier. Every other service trusts the identity this one
issues and stores only a user ID alongside its own data. In return, this
service knows nothing about errands, credit balances, or the supplier
catalogue, and never stores or reasons about them.

## Owner

Zi Yang — one developer owns this folder (root §3); every decision flagged open
below is theirs, not an agent's.

## Boundaries

- **Other services asking about a user.** `order-service` needs "is this
  user a courier?", `credit-service` "does this user exist?", `frontend` a
  display name. Each is a gRPC call, or an event this service publishes — 
  never a join, never a second connection to this database.
- **This service asking about a user's activity.** A rule like "you may not
  switch to requester while a delivery is in flight" is an `order-service`
  question. Either call `order-service` through its generated client or hand
  the rule back to the owner; do not read its tables and do not mirror order
  state here.
- **Balances and credits belong to `credit-service`.** A profile screen
  showing a balance is composed by the caller, not by a column added here.
- **No shared auth package.** If this service and another both need to
  verify a token, the duplication is correct (root §4.2). Do not create a
  shared module or import across service modules.
- **JWT is the recorded identity mechanism.** This service issues and validates
  JWTs for authentication and session handling. Other services will not verify the JWT
  at their own boundaries; they must not connect to this service's database or
  import an auth package. All other services will trust the gRPC metadata (x-user-id and
  x-user-role) from the API Gateway

## Layout

Migrations named `NNNNNN_<name>.{up,down}.sql`.

The request schemas, response schemas, and error contracts (specifically 401 Unauthorized for expired/compromised tokens and 500 Internal Server Error for Redis failures) for /refresh, /logout, and /.well-known/jwks.json must be fully documented in `proto/user/v1/user.proto` to comply with the project's spec-first requirement.

The project uses `protoc` with `protoc-gen-go` and `protoc-gen-go-grpc` to compile the schemas into generated Go code under `internal/gen/user/v1/`. Hand-written DTOs are prohibited; the generated structs must be used globally.

## Packaging

Always try to group all logical units together to reduce coupling and increase cohesion.

In `internal` directory, it should have the following general structure:
```
internal/
|- jwt/                 (contains all jwt service related files)
|- config/              (contains all files related to extracting config information for startup)
|- grpcapi/             (contains gRPC server implementations and interceptors)
   |- server.go         (contains the `Server` struct implementing the generated interface, bridging to domain services)
   |- interceptors/     (contains unary interceptors for request ID injection, structured slog logging, panic recovery, and metadata                     extraction)
<!--
|- httpapi/             (contains dtos, handlers, routers, routes)
   |- router/           (contains `router.go` so that `main.go` can call `router.Setup()`, which in turn calls private function `setUpRoutes` which groups routes obtained from `routes.GetRoutes()` for idiomatic purposes)
   |- routes/           (contains `routes.go` which imports handlers and implements GetRoutes() which returns `func(r chi.Router)` which specifies routes and their respective handlers)
   |- handlers/         (contains `auth_handler.go`, `profile_handler.go` and `system_handler.go` with their own dependencies)
-->
|- repository/          (contains all files related to postgresql database or redis)
|- user/                (contains all domain specific functions, errors, and repository interfaces)
|- hash/                (contains `password.go` for other modules to use its cryptographic functions)
|- session/             (contains `auth.go`, `session.go`, and `invalidation.go` for session lifecycle and login orchestration)
```

<!--
The handlers will be imported by `routes.go` so as a package and instantiated in `GetRoutes()` instead of being passed as instantiations.

All HTTP handler tests must reside directly inside the internal/httpapi/handlers/ directory. They must use package `handlers_test` (black-box testing) to prevent import cycles with the router and to ensure accurate code coverage reporting.
-->

## Bootstrap Admin Decision
The bootstrap.go script must only execute if the users table contains zero admins of any status. Changing INITIAL_ADMIN_* environment variables after the first boot will safely do nothing.

## Networking Decisions
The user-service must only be accessible within the internal Docker network. All external traffic must route exclusively through the API Gateway. (`compose.yaml` should reflect this behaviour)
Use port 8085 for JWKS listener.

## Database Decisions

### PostgreSQL DB
- Table Name: `users`
- Fields:
  - uid: UUID, PRIMARY KEY, unique, not null
  - username: VARCHAR(128), not null
  - email: VARCHAR(255), not null
  - password: VARCHAR(255), not null
  - phone_num: VARCHAR(20), not null
  - date_created: TIMESTAMPTZ, not null
  - last_login_date: TIMESTAMPTZ, NULL
  - account_role: ENUM ('STUDENT' / 'ADMIN'), defaults to STUDENT
  - account_status: ENUM ('ACTIVE' / 'SUSPENDED'), defaults to ACTIVE
  - tokens_valid_after: TIMESTAMPTZ, not null, default NOW()
- Indexes:
    - `CREATE UNIQUE INDEX idx_users_email_lower ON users (LOWER(email));`
    - `CREATE UNIQUE INDEX idx_users_username_lower ON users (LOWER(username));`

- Table Name: `sessions`
- Fields:
  - jti: UUID, PRIMARY KEY, unique, not null
  - created_at: TIMESTAMPTZ, not null
  - expires_at: TIMESTAMPTZ, not null
  - revoked_at: TIMESTAMPTZ, null
  - token_hash: VARCHAR(255), unique, not null
  - replaced_by_token_hash: VARCHAR(255), null
  - uid: UUID, FOREIGN KEY (from `users` schema), not null, ON DELETE CASCADE

`UID` and `date_created` shall be generated using Go (UUID package and time package in the SGT timezone respectively).
Passwords shall be stored securely using bcrypt in the Go implementation.
Store the user model, interface defining database operations, and business logic 
using the repository interface under `internal/user/`. 

### Repository Methods
The `users` repository interface should include these actions: 
1. Create, 
2. GetByID (get user by uuid), 
3. GetByIdentifier (get user by username/email), 
4. Update (update user's mutable fields, which should not allow updating uid or email), 
5. UpdateAccountStatusByID (suspend/reactivate user by their uuid, which should update both `account_status` and `tokens_valid_after` fields in `users` atomically. Note that reactivation should not alter `tokens_valid_after` field) 
6. UpdateLastLoginByID (update the `last_login_date` field atomically without fetching or modifying the core profile data)
7. CountActiveAdmins (During a suspension request, if the target user is an admin and CountActiveAdmins returns 1, the domain service must abort the operation and return a specific domain error (e.g., user.ErrLastAdmin))

Example repository method signatures:
```Go
    Create(ctx context.Context, u *User) error
	GetByID(ctx context.Context, uid uuid.UUID) (*User, error)
	GetByIdentifier(ctx context.Context, identifier string) (*User, error)
	Update(ctx context.Context, u *User) error
	UpdateAccountStatusByID(ctx context.Context, uid uuid.UUID, status AccountStatus) error
    UpdateLastLoginByID(ctx context.Context, uid uuid.UUID, timestamp time.Time) error
    CountActiveAdmins(ctx context.Context) (int, error)
```

The `sessions` repository interface should include these actions: 
1. CreateSession, 
2. GetSessionByHash, 
3. RotateSession, 
4. RevokeSessionByHash, 
5. RevokeAllUserSessions

Example repository method signatures:
```Go
    // CreateSession stores a newly issued refresh token during login or registration.
	CreateSession(ctx context.Context, s *Session) error

	// GetSessionByHash retrieves a session to validate an incoming refresh request.
	GetSessionByHash(ctx context.Context, hash string) (*Session, error)

    // RotateSession handles the entire refresh transaction to prevent race conditions.
    // It MUST execute the following within a SINGLE database transaction (pgx.Tx):
    // 1. SELECT ... FOR UPDATE to lock the old session row.
    // 2. Perform the Replay Detection checks (checking revoked_at and replaced_by_token_hash).
    // 3. If compromised, rollback and return ErrSessionCompromised.
    // 4. If valid, set old session's `revoked_at` = NOW() and `replaced_by_token_hash` = newSession.TokenHash.
    // 5. Insert newSession.
    // 6. Commit the transaction.
	RotateSession(ctx context.Context, oldHash string, newSession *Session) error

	// RevokeSessionByHash is used during a standard logout.
	// It finds the session by its hash and sets `revoked_at` to time.Now().
	RevokeSessionByHash(ctx context.Context, hash string) error

	// RevokeAllUserSessions terminates all sessions of a user.
	// Triggered during ADMIN suspension or when replay detection catches a compromised token.
	RevokeAllUserSessions(ctx context.Context, uid uuid.UUID) error
```

### Redis Configuration
- Environment Variable: REDIS_URL (e.g., redis://localhost:6379/0).
- Use the industry standard [github.com/redis/go-redis/v9](https://github.com/redis/go-redis/v9).
- Key Formats: 
  - JTI Blocklist: `jti:<jti-uuid>`
  - Suspension: `suspended:uid:<user-uuid>`
- Unavailable Behavior: Fail-closed. If a `Set` operation to Redis fails during logout, the application must immediately return a 500 Internal Server Error and abort the subsequent PostgreSQL transaction, forcing the frontend client to retry.
- The user-service should adopt the Gateway's Redis configuration: redis:7-alpine (or 8-alpine depending on Gateway) with `--appendonly yes` and a persistent volume. This guarantees state recovery across container restarts.

## HTTP to gRPC Mapping

* 400 Bad Request → `codes.InvalidArgument` (e.g., malformed UUID, invalid email string).
* 409 last admin conflict → `codes.FailedPrecondition`
* 413 Payload Too Large → `codes.ResourceExhausted` (used by the interceptor to enforce the 10KB limit).
* 429 Too Many Requests → `codes.ResourceExhausted` (for Redis SETNX refresh contention locks).
* 500 Internal Server Error → `codes.Internal` for unexpected Go panics or query logic failures, and `codes.Unavailable` specifically for connection drops to dependencies like PostgreSQL or Redis.

Use a single message with proto3 optional for administrative fields (optional string account_role, optional string account_status, optional google.protobuf.Timestamp date_created).

Use google.protobuf.Timestamp for date_created, last_login_date, and tokens_valid_after

Use native Protobuf enums with a strict zero-value UNSPECIFIED prefix to comply with proto3 rules.
Example: enum AccountRole { ACCOUNT_ROLE_UNSPECIFIED = 0; ACCOUNT_ROLE_STUDENT = 1; ACCOUNT_ROLE_ADMIN = 2; }

Use proto3 optional for fields in UpdateProfileRequest. This generates pointers in Go (e.g., *string), allowing the domain service to distinguish between a field explicitly set to an empty value versus a field the user does not want to update.

Use named empty messages (e.g., message LogoutResponse {} and message UpdateStatusResponse {}) instead of google.protobuf.Empty.

Use the standard grpc.health.v1.Health service exclusively. This service should return status information regarding the whole user service, postgresql database, and redis. Do not define a custom UserService.Check RPC.

Use validation annotations directly in the .proto file (e.g., using buf.build/gen/go/bufbuild/protovalidate)

Use structured google.rpc.Status error details. Base errors will use standard status.Code (e.g., InvalidArgument), but validation failures must attach a google.rpc.BadRequest.FieldViolation detail payload, and domain errors must attach google.rpc.ErrorInfo to provide the frontend with machine-readable causes

Exclude JWKS from the .proto file completely. It must remain a pure HTTP exception handled via a secondary listener in main.go

Authentication Metadata contract: `x-user-id` and `x-user-role` shall be injected by the API Gateway for identity propagation.
The `authorization` metadata shall contain the raw Bearer <token> string, passed by the gateway only for the Logout and Refresh RPCs that require the token payload for blocklisting and DB revocation.

## User Service gRPC RPCs

The service implements the generated gRPC `UserServiceServer` interface. Transport data (tokens) arrives via gRPC Metadata.

- `rpc Register(RegisterRequest) returns (RegisterResponse)`
  * InvalidArgument (400): Returned if validation fails (e.g., email does not end in @u.nus.edu, password is too weak). Includes a 
       google.rpc.BadRequest.FieldViolation detail payload specifying the invalid field.
  * AlreadyExists (409): Returned if the requested username or email is already registered. Includes a google.rpc.ErrorInfo payload specifying the duplication reason.
  * Internal / Unavailable (500/503): Returned for unexpected Go panics or if the PostgreSQL database is unreachable.

- `rpc Login(LoginRequest) returns (LoginResponse)`
  * InvalidArgument (400): Returned if required fields are missing.
  * Unauthenticated (401): Returned for invalid credentials (username/email not found, or password hash mismatch).
  * PermissionDenied (403): Returned if the credentials are correct but the account status is ACCOUNT_STATUS_SUSPENDED.
  * Internal / Unavailable (500/503): Returned for database connection failures.

- `rpc GetProfile(GetProfileRequest) returns (GetProfileResponse)`
  * InvalidArgument (400): Returned if the provided uid string is a malformed UUID.
  * NotFound (404): Returned if the well-formed UUID does not match any existing user.
  * Internal / Unavailable (500/503): Returned for system or database failures.

- `rpc UpdateProfile(UpdateProfileRequest) returns (UpdateProfileResponse)`
  * InvalidArgument (400): Returned for malformed UUIDs or if the optional updated fields (e.g., phone number length, password policy) fail validation. Includes FieldViolation details.
  * Unauthenticated (401): Returned if a user attempting to update their own profile provides an incorrect current_password.
  * PermissionDenied (403): Returned if the x-user-id in the metadata does not match the target uid and the caller lacks the x-user-role of ADMIN.
  * AlreadyExists (409): Returned if updating to a new username or email causes a unique constraint violation in the database.
  * Internal / Unavailable (500/503): Returned for system or database failures.

- `rpc UpdateStatus(UpdateStatusRequest) returns (UpdateStatusResponse)`
  * InvalidArgument (400): Returned for a malformed UUID or if the status enum is ACCOUNT_STATUS_UNSPECIFIED.
  * PermissionDenied (403): Returned if the caller's metadata lacks the ADMIN role.
  * NotFound (404): Returned if the target user ID does not exist.
  * FailedPrecondition (409): Returned if attempting to suspend an admin when CountActiveAdmins equals 1. Includes a google.rpc.ErrorInfo payload indicating the last-admin business rule violation.
  * Internal / Unavailable (500/503): Returned for system or database failures.

- `rpc RefreshSession(RefreshSessionRequest) returns (RefreshSessionResponse)`
  * InvalidArgument (400): Returned if the refresh token string is missing or malformed.
  * Unauthenticated (401): Returned if the token is invalid, expired, or ended by a normal logout. If a compromised replay attack is detected (token already rotated), this status is returned alongside a google.rpc.ErrorInfo payload indicating an ErrSessionCompromised event.
  * ResourceExhausted (429): Returned if the 5-second Redis SETNX lock is already held, indicating parallel refresh contention from the same client.
  * Internal / Unavailable (500/503): Returned if PostgreSQL or Redis are unreachable (triggering a fail-closed response).

- `rpc Logout(LogoutRequest) returns (LogoutResponse)`
  * InvalidArgument (400): Returned if the refresh token in the body is missing or malformed.
  * Unauthenticated (401): Returned if the authorization metadata (Bearer token) is missing or cryptographically invalid. (Note: If the token is valid but the session was already revoked, this RPC succeeds idempotently and returns the empty LogoutResponse).
  * Internal / Unavailable (500/503): Returned for system, database, or Redis blocklist write failures.

**JWKS HTTP Exception:**
The `/.well-known/jwks.json` endpoint CANNOT be converted to gRPC. Because standard JWT verifiers and gateways expect a standard HTTP GET request for key discovery, `main.go` must instantiate a secondary minimal HTTP listener (e.g., standard `net/http` multiplexer) running on a separate port specifically to serve the JWKS JSON payload.

<!--
## User Service API Endpoints
Use Go Chi router for handling API calls. The API should handle these calls:
1. GET /api/v1/health : Returns health status of the whole user microservice (includes Go logic layer and database health)
2. POST /api/v1/users/register : Accepts email, username, and plaintext password. Triggers the repository Create method and publishes an event to the message broker so the Credit Service can allocate the initial credits
3. POST /api/v1/users/login : Accepts an identifier (username or email) and plaintext password. Uses GetByIdentifier to verify credentials and returns JWT authentication/session tokens
   - When a user successfully authenticates, the application service must call `UpdateLastLoginByID`. If this database operation fails, the service must **log the error** (preserving the underlying PostgreSQL failure details and user ID) but **must not fail the login request**. The user must still receive a `200 OK` and their token pair. Tracking login analytics is secondary and must never compromise the availability of the core authentication flow.
4. GET /api/v1/users/{uid} : Uses GetByID to fetch public profile data for a specific user (e.g., when a requester views a courier's profile. Any authenticated user may view another user's `username`, `email`, and `phone_num`, but not `account_role`, `account_status`, or `date_created`, unless the authenticated user is an admin)
5. PUT /api/v1/users/{uid} : Uses Update to modify mutable user profile fields, such as the username or password.
   - For password changes, the endpoint must require the user's currentPassword in the request body alongside the newPassword. Upon a successful password change, the service must immediately revoke all of the user's active sessions (pushing existing JTIs to Redis and revoking refresh tokens in PostgreSQL) to neutralize hijacked sessions.
   - If a user provides an incorrect currentPassword during a profile update, the service must return a 401 Unauthorized with a standardized payload (e.g., {"error": "invalid current password"}). It must not bubble up as a 500 Internal Server Error.
   - If the authenticated principal has the ADMIN role and is modifying a different user's account, the currentPassword requirement must be bypassed. Admins are authorized to force-reset passwords without knowing the target's existing credentials. If an admin is changing their own password, the currentPassword rule still applies.
   - a password change should utilize a mechanism similar to the existing suspension mechanism: the service updates the user's tokens_valid_after field in the users table to the current timestamp. The API Gateway will subsequently reject any existing access tokens where the iat (Issued At) claim is older than this new timestamp. The service must also call RevokeAllUserSessions to immediately invalidate all refresh tokens in PostgreSQL
6. PATCH /api/v1/users/{uid}/status: An ADMIN-only endpoint to transition an account status between ACTIVE and SUSPENDED
   - must reject any attempt to suspend an admin if they are the last ACTIVE admin in the system.
   - Errors: 409 conflict if attempting to suspend the last remaining active admin ({"error": "cannot suspend the last active admin"})
7. POST /api/v1/users/refresh : Accepts a valid refresh token. Issues a new access/refresh token pair and rotates the old refresh token in the database (uses `RotateSession` of the Session Repository's interface)
   - Request: JSON body {"refreshToken": "string"}. No HTTP Authorization header required.
   - Response: 200 OK with JSON {"accessToken": "string", "refreshToken": "string"}.
   - Errors: 401 Unauthorized for an invalid or expired token. If the replay-detection mechanism triggers, return a 401 Unauthorized with a standardized error shape: {"error": "ErrSessionCompromised"}.
8. POST /api/v1/users/logout : Accepts the current access token and refresh token. Pushes the access token's jti to the Redis blocklist and revokes the refresh token in PostgreSQL (by populating the `revoked_at` field). This must be done in the specified order strictly. If Redis fails, the service must abort the PostgreSQL transaction and return a `500 Internal Server Error`. Do not use `DELETE` on the table (uses `RevokeSessionByHash` of the Session Repository's interface)
   - **Execution Sequence & Ownership Verification:**
     1. Hash the incoming refresh token and query PostgreSQL to read the session. If the session is missing or already has a populated `revoked_at` field, immediately return `204 No Content` (idempotent success).
     2. Verify that the session's `uid` matches the verified access token's `sub` claim. If they do not match, reject the request with a `400 Bad Request` (e.g., `{"error": "mismatched session"}`).
     3. Push the access token's `jti` to the Redis blocklist. If the access token is in the 5-second clock-skew window (meaning its theoretical remaining lifetime is ≤ 0), set the Redis TTL to exactly 5 seconds. If the Redis `SET` operation fails, abort the transaction and return a `500 Internal Server Error`.
     4. Revoke the refresh session in PostgreSQL by setting the `revoked_at` field to the current timestamp. Do not use `DELETE`.
   - Request: Standard Authorization: Bearer <access_token> header, plus a JSON body {"refreshToken": "string"}.
   - Response: 204 No Content (empty body) on success, and response should be idempotent should with 204 in case session is already revoked, or when the supplied refresh token does not exist
   - Errors: 500 Internal Server Error if the Redis invalidation fails.
   - Write a minimum-TTL Redis key. Set the Redis TTL to exactly the skew allowance (5 seconds) to guarantee the token is blocked for the remainder of its theoretical accepted life without crashing the Redis SET operation with a negative duration
9. GET /.well-known/jwks.json : the token header and the JWKS payload must include a kid (Key ID) field. The User Service must support loading multiple keys simultaneously: one designated "active" key for signing new tokens, and multiple "retired" keys kept in the JWKS to verify existing tokens until their respective TTLs expire
   - Request: Unauthenticated GET request.
   - Response: 200 OK with the standard JWKS JSON payload containing active and retired public keys.
   - This API endpoint should not be reachable beyond the API gateway. Only the API gateway should be able to reach this endpoint and verify the signatures of incoming JWTs.
-->

<!-- TODO: replace http with gRPC restrictions
Request bodies should enforce a strict 10KB (10 * 1024 bytes) limit using Go's `http.MaxBytesReader(w, r.Body, 10240)`, and return `413 Payload Too Large` and drop the connection if a client exceeds the limit.
-->

Additionally, the gRPC behaviour below should be observed:
* Timeouts: Configure the grpc server with explicit defensive timeouts: ReadHeaderTimeout: 5s, ReadTimeout: 10s, WriteTimeout: 10s, and IdleTimeout: 120s.
* Logging: Log all 500 Internal Server Error events with the underlying Go error message, a stack trace, and a unique Request ID. Never log plaintext passwords, bearer tokens, or PII.
* Health Check: The /api/v1/health endpoint must fail (503 Service Unavailable) if Redis is unreachable. Because the API Gateway strictly depends on the User Service to populate the Redis blocklist and suspension keys, a disconnected Redis instance compromises the platform's security boundary.

Handlers should be grouped according to functions. For example:
1. AuthHandler (includes register, login, refresh, logout)
2. ProfileHandler (includes profile, updateProfile, updateStatus)
3. SystemHandler (includes health and jwks)
Handlers should all be grouped under a package in `httpapi`.

### OpenAPI Specification & API Contracts
The API contract is defined in `api/openapi.yaml` and must strictly align with the handler implementations. The specification must use **OpenAPI 3.1.0**.

**Global API Rules:**
1. **Error Response Schema:** Every `4xx` and `5xx` response across all endpoints must strictly use a standardized `ErrorResponse` component schema: `{"error": "string"}`.
2. **Malformed Identifiers:** Any endpoint accepting a `{uid}` path parameter must return `400 Bad Request` (not `404 Not Found`) if the provided UUID is malformed.
3. **Public Endpoints:** Unauthenticated endpoints (health, register, login, refresh, jwks) must explicitly declare `security: []` in the OpenAPI spec to override any global security requirements.

**Endpoint-Specific Contracts:**
*   **GET /api/v1/health**
    *   **Statuses:** `200 OK` {"status":"ok"}, `503 Service Unavailable` {"status:"unhealthy"} (e.g., when Redis is unreachable).
*   **POST /api/v1/users/register**
    *   **Statuses:** `201 Created`, `400 Bad Request`, `409 Conflict`, `500 Internal Server Error`.
    *   **Schema Constraints:** The `email` field must explicitly document the `@u.nus.edu` restriction. The `phone_num` field must have `maxLength: 20`.
*   **POST /api/v1/users/login**
    *   **Statuses:** `200 OK`, `400 Bad Request`, `401 Unauthorized` (invalid credentials), `403 Forbidden` (account suspended), `500 Internal Server Error`.
    *   **Schema Constraints:** Remove password length validation (8-128) from the `LoginRequest` schema. Login handlers must not enforce length limits on input; they simply pass the input to the hash comparator which will inherently fail invalid strings, returning `401`.
*   **GET /api/v1/users/{uid}**
    *   **Statuses:** `200 OK`, `401 Unauthorized`, `404 Not Found`, `500 Internal Server Error`.
*   **PUT /api/v1/users/{uid}**
    *   **Statuses:** `200 OK`, `401 Unauthorized`, `403 Forbidden`, `404 Not Found`, `409 Conflict`, `500 Internal Server Error`.
    *   **Schema Constraints:** The `UpdateProfileRequest.password` field must declare the 8-128 character policy.
    *   **Authorization Fix:** The code must be updated to align with the spec description. It must read the `X-User-ID` header injected by the API Gateway to verify the caller's identity, instead of attempting to read a Bearer token directly.
*   **PATCH /api/v1/users/{uid}/status**
    *   **Statuses:** `200 OK`, `400 Bad Request`, `401 Unauthorized`, `403 Forbidden`, `404 Not Found`, `409 Conflict` (last admin), `500 Internal Server Error`.
*   **POST /api/v1/users/refresh**
    *   **Statuses:** `200 OK`, `400 Bad Request`, `401 Unauthorized`, `429 Too Many Requests`, `500 Internal Server Error`.
*   **POST /api/v1/users/logout**
    *   **Statuses:** `204 No Content`, `400 Bad Request` (mismatched session), `401 Unauthorized`, `500 Internal Server Error`.
*   **GET /.well-known/jwks.json**
    *   **Statuses:** `200 OK`, `500 Internal Server Error`.
    *   **Schema Constraints:** The JWKS key object schema must explicitly require the `kid` property.

### JWT Signing, Key Policy, and Trust Boundary
- Signing Algorithm & Key Type: RS256, the user service shall hold the private key to sign tokens, while the API gateway
uses the public key to verify them.
- Key Distribution & Rotation: via a standard /.well-known/jwks.json endpoint exposed by the User Service
- Clock skew: 5 seconds to prevent false rejections due to server clock drift
- Trust boundary: internal services (Supplier, Order, Credit) do not accept or parse JWTs. They strictly trust and accept the X-User-ID and X-User-Role HTTP headers injected by the API Gateway after it verifies the token
- TTL: The default configurations are: JWT_ACCESS_TOKEN_TTL should be 15 minutes and JWT_REFRESH_TOKEN_TTL should be 7 days

### JWT Production Key Loading
- Key Format: PEM/string/file
- derive the `kid` programmatically on startup by computing the SHA-256 fingerprint of the public key's DER encoding
- Retired Keys: Supply multiple active and retired keys using a mounted JSON configuration file (e.g., keys.json) containing an array of PEM strings. The Go service parses all keys on startup, hashes them to generate their respective `kid`s, uses the newest one for signing, and exposes all of them via the /.well-known/jwks.json endpoint.
- Store JWT_ACCESS_TOKEN_TTL and JWT_REFRESH_TOKEN_TTL as standard Go duration strings (e.g., "15m", "168h"). Parse them securely at startup using time.ParseDuration()

The active key is determined by array order as follows:
- Index [0] (The Active Key): The first key in the array is always the active signing key. The User Service uses this key to sign all new Access and Refresh tokens generated during login and token refresh requests.
- Index [1..N] (The Retired Keys): All subsequent keys in the array are classified as "retired." They are never used to sign new tokens.

### Replay detection
If a refresh token is presented that already has a populated `revoked_at` or `replaced_by_token_hash` field in the `sessions` table,
the system must assume a compromise and revoke all tokens associated with that user.

Implementation:
1. Hash the incoming token
2. Call `GetSessionByHash` (must be executed within an active transaction with `SELECT ... FOR UPDATE`).
3. Evaluate the returned Session state:
    - **Valid:** If `RevokedAt` is NULL and `ReplacedByTokenHash` is NULL, the token is valid. Proceed with rotation.
    - **Logged Out (Not Compromised):** If `RevokedAt` is NOT NULL but `ReplacedByTokenHash` is NULL, the token was ended via normal logout. Return `401 Unauthorized` (invalid token) but **do not** revoke other sessions.
    - **Replay Attack (Compromised):** If `ReplacedByTokenHash` is NOT NULL, the token was previously used to issue a new session. This indicates a token theft or race condition. Call `RevokeAllUserSessions()` and return `401 Unauthorized (ErrSessionCompromised)`.

The database transaction used to check the token in the `sessions` database should use Row-level locking (e.g.
using `SELECT ... FOR UPDATE` when checking the token.

A normal logout (where a token is manually revoked) must not trigger a full account lockout if that specific token is accidentally submitted again (e.g., by a stale browser tab). ErrSessionCompromised should only fire if a token was rotated (i.e., replaced_by_token_hash is populated) and then reused.

To prevent race conditions where simultaneous refresh requests from the same client cause one to succeed and the other to falsely trigger replay detection, the refresh endpoint must acquire a short-lived Redis lock on the session hash before executing the rotation.

### Parallel Refresh Lock & Replay Detection
Both a Redis lock and a PostgreSQL transaction lock are required during the refresh flow.

**Implementation Sequence:**
1. **Acquire Redis Lock (Debouncing):** Before querying PostgreSQL, attempt to create a Redis key (e.g., `refresh_lock:<refresh_token_hash>`) using a `SETNX` (Set if Not Exists) operation.
    - **TTL:** The lock must have a strict TTL of **5 seconds**.
    - **Concurrency Rejection:** If the lock already exists, a parallel request is currently processing this exact token. Immediately return `429 Too Many Requests`. Do not execute the PostgreSQL query. This prevents accidental self-nuking.
    - **Unavailable Behavior:** If Redis is unreachable, fail-closed. Abort the request and return `500 Internal Server Error`.
2. **Execute PostgreSQL Transaction (Integrity):** Once the Redis lock is acquired, invoke `RotateSession`. This method must open a single transaction, use `SELECT ... FOR UPDATE` to read the row, evaluate the compromise rules (`ReplacedByTokenHash != NULL`), and perform the rotation.
3. **Release:** The Redis lock may be explicitly deleted after the database transaction completes, or simply left to expire via its 5-second TTL.

### Implementation of Access and Refresh Tokens
The Access/Refresh Token Claims should include:
- jti (unique uuid of the token for targeted revocation)
- iss ('campusrun-user-service')
- sub (uuid of user)
- aud ('campusrun-api')
- exp (unix timestamp to enforce lifespan)
- iat (unix timestamp of creation)
- typ (either "access" or "refresh")
- role (ONLY FOR Access Token Claims. Should include user's permission level -- STUDENT or ADMIN)

The user service should store a hash of the refresh token in the database (which can last 7 days). The refresh token
will be sent from the user for verification against the database to obtain another access token. 

### Flow of authentication
First and foremost, the API Gateway will be implemented in another folder separate from the user service.
The API Gateway will sit between the user's browser and all microservices. The API Gateway will verify the signature of the JWT (using public keys provided by user service via `/.well-known/jwks.json` endpoint) without querying the user microservice, unless the user's browser submits credentials for login, registration, token refresh, or logout.
It will read from a Redis Blocklist to verify the access token (specifically the `jti` claim) is valid (i.e. user has not logged out) and check Redis for user suspension key (formatted as `suspended:uid:<user-uuid>`) using the `sub` claim to verify user has not been suspended by an admin. It will then inject the claims in headers before forwarding requests to internal APIs via REST calls.
The API Gateway should strip any incoming `X-User-ID` and `X-User-Role` headers before injecting its own validated claims (`sub` and `role`) and ensure that all microservices are in a private VPC inaccessible from the Internet.
Note: If the Gateway cannot reach Redis to check the blocklist and suspensions, the request must be rejected.
Note: If PostgreSQL fails after redis suspension invalidation and refresh-session revocation, the user service should return a `500 Internal Server Error` forthe frontend client to retry the logout request. Upon receiving the retry, the user service should overwrite the exact same Redis key with the identical TTL to ensure system recovers when PostgreSQL successfully commits the transaction.

When the user logs out, the frontend will send a request (containing access and refresh tokens) to the API gateway, which will be passed to the user service to invalidate the session by pushing the access token's `jti` to the redis blocklist and then revoking the refresh token from its own `sessions` database. 
When an `ADMIN` suspends an account, the user service shall push a Redis key (e.g., `suspended:uid:<user-uuid>`) containing the current time, and then update a `tokens_valid_after` field containing the same timestamp. The API Gateway will check if a suspended key exists for the incoming `sub`. If it does, and the token's `iat` (Issued At) claim is older (earlier) than the `tokens_valid_after` timestamp, the token is rejected.

The API Gateway is a reader of the Redis blocklist, and the user service shall be the sole writer to the Redis blocklist.
(The API Gateway code should fit to this behaviour)
The TTL for a specific `jti` in Redis must exactly match the remaining time until that access token's exp timestamp. The TTL for a suspended:uid:<uuid> key must match the JWT_ACCESS_TOKEN_TTL duration.

### Username/Password Policy
The username policy as stated in the product backlog is:
- only contains alphanumeric characters (Explicitly restrict "alphanumeric" to strictly ASCII characters (^[a-zA-Z0-9]+$) to prevent Unicode homoglyph spoofing (e.g., Cyrillic 'а'). Uniqueness must be case-insensitive, though display casing can be preserved)
- maximum 128 characters
The password policy as stated in the product backlog is:
- 8 – 128 characters (pre-hash the password with SHA-256 before passing to bcrypt to fit the 72 bytes limit)
- at least one uppercase letter
- at least one lowercase letter
- at least one digit
This policy should be checked during registration and updating profile of a user.

### Email Policy
Pass the input to Go's `net/mail` package and pass the input to `mail.ParseAddress()` to prevent malformed inputs

### Observability & Error Handling
To ensure secure and traceable operational logging, the service must not rely on global loggers or silently discard underlying errors.

- **Unary Interceptors:** Use gRPC Unary Interceptors exclusively. No stream interceptors are permitted.
- **Metadata Extraction:** Interceptors must extract `x-user-id`, `x-user-role`, and `authorization` from `metadata.FromIncomingContext(ctx)`.
- **Centralized Error Mapping:** A dedicated interceptor must act as the error boundary, converting core domain errors (e.g., `user.ErrNotFound`) into appropriate `google.golang.org/grpc/status` codes. Unhandled panics must be recovered here, capturing `runtime/debug.Stack()`, logging via injected `slog`, and returning `codes.Internal`.
<!--
- **Request IDs:** Use `github.com/go-chi/chi/v5/middleware.RequestID` globally in the Chi router. This automatically generates and propagates a unique request ID into every `http.Request` context.
-->
- **Logger Injection:** The application must use structured logging (e.g., Go 1.21's `log/slog.Logger`). The logger instance must be instantiated in `main.go` and explicitly injected into the handler constructors (e.g., `NewAuthHandler(..., logger *slog.Logger)`) to avoid mutable global state.
- **Error Exposure & Stack Traces:** Handlers must never discard original errors. To standardize this, the HTTP transport layer must implement a centralized helper function (e.g., `writeError(ctx context.Context, w http.ResponseWriter, logger *slog.Logger, status int, rawErr error, clientMsg string)`).
    - For `500 Internal Server Error` statuses, this helper must extract the Request ID from the context (`middleware.GetReqID(ctx)`) and capture the stack trace using `runtime/debug.Stack()`.
    - The helper must structured-log the Request ID, the raw underlying error, and the stack trace directly to the standard output.
    - After successfully logging the raw details, it must safely write the sanitized JSON response (e.g., `{"error": "Internal Server Error"}`) to the client.

### First Admin creation
`INITIAL_ADMIN_EMAIL`, `INITIAL_ADMIN_USERNAME`, `INITIAL_ADMIN_PASSWORD` should be configured in `.env` 
for user service to read. There should be a `bootstrap.go` file in `cmd/api/` alongside `main.go` to
initialise the `users` table with a default admin. This bootstrap step shall be done after user repository and account service
initialisation and before the Chi router is configured. Ensure that the admin credentials will not be leaked
into production.

### Entry point of service (`cmd/api/main.go`)
The `main.go` file should construct its dependencies in the following order:
1. Parse environment variables (using a library like kelseyhightower/envconfig).
2. Initialize the PostgreSQL pgxpool and the go-redis client.
3. Initialize the JWT Key Manager (parsing PEMs and generating kids).
4. Instantiate the Repositories (UserRepository, SessionRepository).
5. Add default admin account if `users` table does not have at least 1 user with ADMIN role.
6. Instantiate the background Outbox worker goroutine. (to be done at the last stage of the project)
7. Instantiate the Domain Service (injecting repos, redis client, and JWT manager).
<!-- 8. Instantiate the HTTP Handlers and mount them to the Chi router. -->
8. Instantiate the gRPC server (`grpc.NewServer()`) with the chained Unary Interceptors.
9. Register the `UserServiceServer` and `grpc_health_v1` server.
10. Bind a `net.Listen("tcp", ":<port>")` listener, start the gRPC server, and configure `grpcServer.GracefulStop()` on OS interrupt signals.
11. Start the secondary HTTP server for the JWKS endpoint on a separate port.

### Miscellaneous details

In the case where the PostgreSQL fails after the Redis operation completes the invalidation, the user service should:
1. Returns a 500 Internal Server Error.
2. The frontend (which still holds the valid refresh token) automatically retries the logout request. The Go service overwrites the exact same Redis key with the same TTL, and the database eventually commits.

Implementation for event sending to an event service shall also be deferred

## Local development

Port **8081**, database URL `USER_DB_URL` (root §3). Also the JWT settings
used for JWT authentication and session handling and already present in the
root `.env.example` — `JWT_KEYSET_PATH`, `JWT_ACCESS_TOKEN_TTL`, `JWT_REFRESH_TOKEN_TTL`.

Developers should create a local `keys.json` file containing an array of PEM-encoded private keys, and to set `JWT_KEYSET_PATH` to point to it. The JSON file should contain only private keys, and the system will programmatically derive the public keys and kid for each entry (by computing the SHA-256 fingerprint of the extracted public key's DER encoding) on startup.

The JSON file should contain a single root with a keys array. Here is an example:
```json
{
  "keys": [
    "-----BEGIN PRIVATE KEY-----\nMIIEvAIBADANBgkqhkiG9w0BAQEFAASCBKYwggSiAgEAAoIBAQ...\n-----END PRIVATE KEY-----",
    "-----BEGIN PRIVATE KEY-----\nMIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQ...\n-----END PRIVATE KEY-----"
  ]
}
```

```bash
go run ./cmd/api      # migrations run on startup via golang-migrate
go test ./...
```

### Key Generation Script (`generate_jwks.py`)
To facilitate local development and JWT integration, the service includes a Python script to generate the required RSA private keys. This script is a strict build dependency and must adhere to the following rules:

*   **Product Naming:** All descriptions, comments, and CLI outputs within the script must use "CampusRun" to align with the `campusrun-user-service` token issuer and audience claims.
*   **Strict Permission Enforcement:** The script must explicitly apply `0600` (owner read/write only) permissions to `keys.json` on *every* run using `os.fchmod(fd, 0o600)`. Relying on `os.open` creation modes is insufficient, as it leaves existing wider permissions intact.
*   **Pipeline Failure on Error:** If the script encounters an error while generating or writing the keys, it must print the error and terminate with a non-zero exit code (e.g., `sys.exit(1)`). It must not swallow the error and exit `0`.
*   **Local Dependencies:** Python 3 and the `cryptography` package are official prerequisites for building and running the user-service locally.
*   **AI Disclosure:** If the script was generated by an AI, it must include the standard AI disclosure header at the top of the file and be recorded in the repository's usage log.

## Gotchas

- **Credentials live here, so this is the highest-risk code in the repo.**
  Password storage and JWT handling use vetted libraries (for example,
  `golang.org/x/crypto` for bcrypt and a maintained JWT library) — no
  home-rolled crypto. Never log a password, hash, token or keys in `JWT_KEYSET_PATH`; keep
  them out of DTOs and errors. JWT session invalidation must be represented by
  an explicit service boundary; do not silently assume that stateless token
  expiry alone satisfies logout or suspension requirements.
- **Role changes are named operations, not a boolean parameter.** A
  `SetRole(ctx, id, isAdmin bool)`-shaped API is control coupling (root §5);
  the requester/courier toggle and any admin grant are separate, separately
  authorised functions.
