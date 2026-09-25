# User Service

The identity authority for the platform (mandatory requirement **M2**):
accounts, credentials, sessions/tokens, profile information, and the role a
person acts in — requester, courier, or admin — including the toggle between
requester and courier. Every other service trusts the identity this one
issues and stores only a user ID alongside its own data. In return, this
service knows nothing about errands, credit balances, or the supplier
catalogue, and never stores or reasons about them.

**Status: Implementing on `feat/user-service` branch.**

## Owner

Zi Yang — one developer owns this folder (root §3); every decision flagged open
below is theirs, not an agent's.

## Boundaries

- **Other services asking about a user.** `order-service` needs "is this
  user a courier?", `credit-service` "does this user exist?", `frontend` a
  display name. Each is an HTTP call through the generated OpenAPI client,
  or an event this service publishes — never a join, never a second
  connection to this database.
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
  import an auth package. All other services will trust the `X-User-ID` and
  `X-User-Role` headers from the API Gateway

## Layout

Mirror `supplier-service` package for package (root §6 and that folder's
`AGENTS.md`); module path `foc/user-service`; migrations named
`NNNNNN_<name>.{up,down}.sql`.

Which domain packages exist under `internal/`, and what each contains, is the
owner's to decide — `supplier-service` has exactly one (`internal/supplier`)
because it has one aggregate; identity may not.

Root §8 makes `api/openapi.yaml` the contract; `supplier-service` predates it
and has no `api/` directory yet. Adding one here is an interface decision: the
owner writes the spec, an agent implements against it.

The request schemas, response schemas, and error contracts (specifically 401 Unauthorized for expired/compromised tokens and 500 Internal Server Error for Redis failures) for /refresh, /logout, and /.well-known/jwks.json must be fully documented in `api/openapi.yaml` to comply with the project's spec-first requirement.

## Packaging

Always try to group all logical units together to reduce coupling and increase cohesion.

In `internal` directory, it should have the following general structure:
```
internal/
|- jwt/                 (contains all jwt service related files)
|- config/              (contains all files related to extracting config information for startup)
|- httpapi/             (contains dtos, handlers, routers, routes)
   |- router/           (contains `router.go` so that `main.go` can call `router.Setup()`, which in turn calls private function `setUpRoutes` which groups routes obtained from `routes.GetRoutes()` for idiomatic purposes)
   |- routes/           (contains `routes.go` which imports handlers and implements GetRoutes() which returns `func(r chi.Router)` which specifies routes and their respective handlers)
   |- handlers/         (contains `auth_handler.go`, `profile_handler.go` and `system_handler.go` with their own dependencies)
|- repository/          (contains all files related to postgresql database or redis)
|- user/                (contains all domain specific functions, errors, and repository interfaces)
|- hash/                (contains `password.go` for other modules to use its cryptographic functions)
|- session/             (contains `auth.go`, `session.go`, and `invalidation.go` for session lifecycle and login orchestration)
```

The handlers will be imported by `routes.go` so as a package and instantiated in `GetRoutes()` instead of being passed as instantiations.

## Database Decisions

### PostgreSQL DB
- Table Name: `users`
- Fields:
  - uid: UUID, PRIMARY KEY, unique, not null
  - username: VARCHAR(128), unique, not null
  - email: VARCHAR(255), unique, not null
  - password: VARCHAR(255), not null
  - phone_num: VARCHAR(20), not null
  - date_created: TIMESTAMPTZ, not null
  - last_login_date: TIMESTAMPTZ, NULL
  - account_role: ENUM ('STUDENT' / 'ADMIN'), defaults to STUDENT
  - account_status: ENUM ('ACTIVE' / 'SUSPENDED'), defaults to ACTIVE
  - tokens_valid_after: TIMESTAMPTZ, not null, default NOW()

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

Example repository method signatures:
```Go
    Create(ctx context.Context, u *User) error
	GetByID(ctx context.Context, uid uuid.UUID) (*User, error)
	GetByIdentifier(ctx context.Context, identifier string) (*User, error)
	Update(ctx context.Context, u *User) error
	UpdateAccountStatusByID(ctx context.Context, uid uuid.UUID, status AccountStatus) error
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

	// RotateSession atomically invalidates the old refresh token and issues a new one.
	// It must execute the following as a single database transaction:
	// 1. Set old session's `revoked_at` to time.Now()
	// 2. Set old session's `replaced_by_token_hash` to newSession.TokenHash
	// 3. Insert newSession
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

## User Service API Endpoints
Use Go Chi router for handling API calls. The API should handle these calls:
1. GET /api/v1/health : Returns health status of the whole user microservice (includes Go logic layer and database health)
2. POST /api/v1/users/register : Accepts email, username, and plaintext password. Triggers the repository Create method and publishes an event to the message broker so the Credit Service can allocate the initial credits
3. POST /api/v1/users/login : Accepts an identifier (username or email) and plaintext password. Uses GetByIdentifier to verify credentials and returns JWT authentication/session tokens
4. GET /api/v1/users/{uid} : Uses GetByID to fetch public profile data for a specific user (e.g., when a requester views a courier's profile. Any authenticated user may view another user's `username`, `email`, and `phone_num`, but not `account_role`, `account_status`, or `date_created`, unless the authenticated user is an admin)
5. PUT /api/v1/users/{uid} : Uses Update to modify mutable user profile fields, such as the username or password.
6. PATCH /api/v1/users/{uid}/status: An ADMIN-only endpoint to transition an account status between ACTIVE and SUSPENDED
7. POST /api/v1/users/refresh : Accepts a valid refresh token. Issues a new access/refresh token pair and rotates the old refresh token in the database (uses `RotateSession` of the Session Repository's interface)
   - Request: JSON body {"refreshToken": "string"}. No HTTP Authorization header required.
   - Response: 200 OK with JSON {"accessToken": "string", "refreshToken": "string"}.
   - Errors: 401 Unauthorized for an invalid or expired token. If the replay-detection mechanism triggers, return a 401 Unauthorized with a standardized error shape: {"error": "ErrSessionCompromised"}.
8. POST /api/v1/users/logout : Accepts the current access token and refresh token. Pushes the access token's jti to the Redis blocklist and revokes the refresh token in PostgreSQL (by populating the `revoked_at` field). This must be done in the specified order strictly. If Redis fails, the service must abort the PostgreSQL transaction and return a `500 Internal Server Error`. Do not use `DELETE` on the table (uses `RevokeSessionByHash` of the Session Repository's interface)
   - Request: Standard Authorization: Bearer <access_token> header, plus a JSON body {"refreshToken": "string"}.
   - Response: 204 No Content (empty body) on success.
   - Errors: 500 Internal Server Error if the Redis invalidation fails.
9. GET /.well-known/jwks.json : the token header and the JWKS payload must include a kid (Key ID) field. The User Service must support loading multiple keys simultaneously: one designated "active" key for signing new tokens, and multiple "retired" keys kept in the JWKS to verify existing tokens until their respective TTLs expire
   - Request: Unauthenticated GET request.
   - Response: 200 OK with the standard JWKS JSON payload containing active and retired public keys.
   - This API endpoint should not be reachable beyond the API gateway. Only the API gateway should be able to reach this endpoint and verify the signatures of incoming JWTs.

Handlers should be grouped according to functions. For example:
1. AuthHandler (includes register, login, refresh, logout)
2. ProfileHandler (includes profile, updateProfile, updateStatus)
3. SystemHandler (includes health and jwks)
Handlers should all be grouped under a package in `httpapi`.

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
2. Call `GetSessionByHash`. If it returns `ErrSessionNotFound`, reject the request to refresh the session.
3. Check the returned `Session.RevokedAt`
   - If `RevokedAt` is NULL, the token is valid, and the system should call `RotateSession`.
   - If `RevokedAt` is NOT NULL, then the token has been leaked or there is a replay attack. Call `RevokeAllUserSessions()`
     and return `401 Unauthorized (ErrSessionCompromised)` 

The database transaction used to check the token in the `sessions` database should use Row-level locking (e.g.
using `SELECT ... FOR UPDATE` when checking the token.

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

When the user logs out, the frontend will send a request (containing access and refresh tokens) to the API gateway, which will be passed to the user service to invalidate the session by pushing the access token's `jti` to the redis blocklist and then revoking the refresh token from its own `sessions` database. 
When an `ADMIN` suspends an account, the user service shall push a Redis key (e.g., `suspended:uid:<user-uuid>`) containing the current time, and then update a `tokens_valid_after` field containing the same timestamp. The API Gateway will check if a suspended key exists for the incoming `sub`. If it does, and the token's `iat` (Issued At) claim is older (earlier) than the `tokens_valid_after` timestamp, the token is rejected.

The API Gateway is a reader of the Redis blocklist, and the user service shall be the sole writer to the Redis blocklist.
The TTL for a specific `jti` in Redis must exactly match the remaining time until that access token's exp timestamp. The TTL for a suspended:uid:<uuid> key must match the JWT_ACCESS_TOKEN_TTL duration.

### Username/Password Policy
The username policy as stated in the product backlog is:
- only contains alphanumeric characters
- maximum 128 characters
The password policy as stated in the product backlog is:
- 8 – 128 characters
- at least one uppercase letter
- at least one lowercase letter
- at least one digit
This policy should be checked during registration and updating profile of a user.

### First Admin creation
`INITIAL_ADMIN_EMAIL`, `INITIAL_ADMIN_USERNAME`, `INITIAL_ADMIN_PASSWORD` should be configured in `.env` 
for user service to read. There should be a `bootstrap.go` file in `cmd/api/` alongside `main.go` to
initialise the `users` table with a default admin. This bootstrap step shall be done after user repository
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
8. Instantiate the HTTP Handlers and mount them to the Chi router.

### Project sequencing decision

Suspension Redis invalidation is explicitly deferred to the final implementation
iteration of the project. Until that iteration, account suspension may update
PostgreSQL but must not be treated as fully effective for already-issued access
tokens; the gateway-side Redis suspension check remains incomplete. No earlier
iteration should silently substitute a different invalidation mechanism.

In the case where the PostgreSQL fails after the Redis operation completes the invalidation, the user service should:
1. Returns a 500 Internal Server Error.
2. The frontend (which still holds the valid refresh token) automatically retries the logout request. The Go service overwrites the exact same Redis key with the same TTL, and the database eventually commits.

Event sending to an event service shall also be deferred

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
go test -tags=integration ./...
```

A `user-service` / `user-db` pair in the root `compose.yaml` does not exist
yet; adding it edits a shared file, so flag it (root §3).

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
- **How the first admin comes into existence is an open decision** for the
  owner. If a task implies one, stop and ask rather than picking. Flag the
  security implications to the human; do not settle them yourself.
