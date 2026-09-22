// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-19
// Scope: Auth client — fixture and gateway implementations side by side.
//   2026-09-20: login now takes an identifier (F1.2.1), registration returns a
//   pending registration rather than a session (F1.1.2.7), and OTP verify and
//   resend were added (F1.1.2.4-F1.1.2.6). 2026-09-21: the gateway client
//   was reconciled with user-service's committed api/openapi.yaml — login
//   builds its session from token claims plus the profile endpoint, logout
//   sends the refresh token, refresh returns the rotated pair, and the
//   three OTP calls are blocked because no endpoint implements them.
//   2026-09-21 (later): the session now carries email and contact, which
//   user-service's UserResponse/RestrictedUserResponse gained in 72fe5a5.
// Author review: PENDING — <reviewer to complete>

import { config } from "../../lib/config";
import { NetworkError, send } from "../../lib/http";
import { mockDelay } from "../../lib/mock";
import { readAccessTokenClaims } from "../../lib/jwt";
import type { TokenPair } from "../../lib/tokens";
import * as fixtureOtp from "./fixtureOtp";
import type { AccountRole, PendingRegistration, Session } from "./types";
import { validateOtp, validateRegistration } from "./validation";

/**
 * Two clients live here, and App.tsx picks one at wiring time — deliberately
 * not a `useMock` flag passed into a single function, which would be the
 * control coupling root AGENTS.md §5 calls out.
 *
 *   - logIn / signUp / verifyRegistration / ...   fixtures. Active today.
 *   - logInViaGateway / ...                       the real flow. Inert until
 *                                                 the gateway serves auth.
 *
 * THE REQUEST AND RESPONSE SHAPES BELOW ARE NOT A CONTRACT.
 *
 * The gateway's public auth paths ARE now recorded (ai/decisions.md D-027), so
 * ROUTES below is no longer guesswork for login, register, refresh and logout.
 * The OTP endpoints are a different matter: the D1 backlog requires the
 * behaviour (F1.1.2.3-F1.1.2.7) but no spec defines the calls, so those two
 * paths remain placeholders with the right surface, to be REPLACED by the
 * generated client once user-service's owner writes them.
 */

/** Recorded in D-027, except where marked. */
const ROUTES = {
  login: "/auth/login",
  register: "/auth/register",
  refresh: "/auth/refresh",
  logout: "/auth/logout",
  /** UNRECORDED — behaviour is required by F1.1.2.7, the call is not specced. */
  verify: "/auth/register/verify",
  /** UNRECORDED — behaviour is required by F1.1.2.4, the call is not specced. */
  resend: "/auth/register/resend",
  /**
   * D-027's prefix proxy. `{uid}` is appended; the gateway rewrites this onto
   * user-service's `/api/v1/users/{uid}`.
   */
  profile: "/api/users",
} as const;

export class AuthError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "AuthError";
  }
}

/**
 * F1.2.4 — a suspended account must be told it is suspended, which is a
 * different outcome from bad credentials (F1.2.3, a deliberately generic
 * error). The caller renders these differently, so the distinction survives
 * as a type rather than as string matching.
 */
export class SuspendedAccountError extends AuthError {
  constructor(message = "This account is suspended. Contact an administrator.") {
    super(message);
    this.name = "SuspendedAccountError";
  }
}

/** What a successful login yields: who you are, plus the pair from D-011. */
export interface AuthResult {
  session: Session;
  tokens: TokenPair;
}

function initialsOf(value: string): string {
  const cleaned = value.replace(/[^a-zA-Z0-9]+/g, " ").trim();
  if (!cleaned) return "NU";
  const parts = cleaned.split(/\s+/);
  if (parts.length === 1) return parts[0]!.slice(0, 2).toUpperCase();
  return parts
    .slice(0, 2)
    .map((part) => part[0]!.toUpperCase())
    .join("");
}

function usernameFromEmail(email: string): string {
  const local = email.split("@")[0] ?? "student";
  return local.replace(/[^a-zA-Z0-9]+/g, "") || "student";
}

// ---------------------------------------------------------------------------
// Fixtures — active while user-service does not exist
// ---------------------------------------------------------------------------

function fixtureSession(
  email: string,
  username: string,
  contact = "",
  role: AccountRole = "STUDENT",
): Session {
  return {
    userId: "mock-user-1",
    username,
    email,
    contact,
    // F1.5.1 — every newly registered account is a STUDENT.
    role,
    initials: initialsOf(username),
  };
}

function fixtureResult(session: Session): AuthResult {
  return {
    session,
    tokens: { accessToken: "mock-access-token" },
  };
}

/**
 * MOCK. Authenticates nothing. Accepts any identifier, ignores the password
 * entirely, and hands back a fixture session with obviously-fake tokens. No
 * credential is transmitted, stored or checked.
 *
 * F1.2.1 — the identifier is a username OR an NUS email, so this takes
 * whichever the user typed and does not insist on an email.
 */
export async function logIn(identifier: string): Promise<AuthResult> {
  const trimmed = identifier.trim();
  if (!trimmed) throw new AuthError("Enter your username or NUS email.");

  // A reserved identifier so the suspended-account path (F1.2.4) can actually
  // be seen and styled before user-service exists.
  if (trimmed.toLowerCase() === "suspended") {
    throw new SuspendedAccountError();
  }

  const isEmail = trimmed.includes("@");
  const email = isEmail ? trimmed : `${trimmed}@u.nus.edu`;
  const username = isEmail ? usernameFromEmail(trimmed) : trimmed;
  return mockDelay(fixtureResult(fixtureSession(email, username)));
}

/**
 * MOCK. F1.1.2.7 — this does NOT produce a session. It opens a registration
 * and returns the pending state; only verifyRegistration completes it.
 */
export async function signUp(
  email: string,
  username: string,
  password: string,
  _contact: string,
): Promise<PendingRegistration> {
  const problem = validateRegistration({ email, username, password });
  if (problem) throw new AuthError(problem);
  return mockDelay(fixtureOtp.startRegistration(email.trim(), username.trim()));
}

/** MOCK. F1.1.2.7 — completes registration against the issued code. */
export async function verifyRegistration(
  pendingRegistration: PendingRegistration,
  code: string,
  contact: string,
): Promise<AuthResult> {
  const problem = validateOtp(code);
  if (problem) throw new AuthError(problem);
  const { username, email } = fixtureOtp.verify(
    pendingRegistration.handle,
    code,
  );
  return mockDelay(fixtureResult(fixtureSession(email, username, contact)));
}

/** MOCK. F1.1.2.4-F1.1.2.6 — replacement code, with the limits applied. */
export async function resendOtp(
  pendingRegistration: PendingRegistration,
): Promise<PendingRegistration> {
  return mockDelay(fixtureOtp.resend(pendingRegistration.handle));
}

/** MOCK. No token was ever real, so there is nothing to revoke. */
export async function logOut(): Promise<void> {
  return mockDelay(undefined);
}

/**
 * MOCK. The fixture access token never expires, so this is never reached in
 * practice. It exists so the fixture and gateway clients have the same
 * surface and App.tsx can swap one for the other in a single line.
 */
export async function refreshAccessToken(): Promise<TokenPair> {
  return mockDelay({ accessToken: "mock-access-token" });
}

// ---------------------------------------------------------------------------
// Gateway-backed — inert until api-gateway serves the auth routes
// ---------------------------------------------------------------------------

function decodeAuthError(body: unknown, status: number): AuthError {
  // Not assuming a shared error envelope (frontend/AGENTS.md, Gotchas):
  // supplier-service returns {"error": "..."}, and whether user-service and
  // the gateway match is their owners' decision. Read it if it is there.
  const message =
    body && typeof body === "object" && "error" in body
      ? String((body as { error: unknown }).error)
      : null;

  // F1.2.4 — suspension is a distinct outcome the UI must name. How
  // user-service signals it is not recorded, so this reads the two signals it
  // could plausibly send and falls back to the generic error otherwise.
  const code =
    body && typeof body === "object" && "code" in body
      ? String((body as { code: unknown }).code).toUpperCase()
      : "";
  if (status === 403 || code.includes("SUSPEND")) {
    return new SuspendedAccountError(message ?? undefined);
  }
  if (message) return new AuthError(message);
  // F1.2.3 — a generic error for bad credentials, naming neither field.
  if (status === 401) return new AuthError("Those details did not match.");
  return new AuthError(`Sign-in failed (${status}).`);
}

/**
 * Reads what the gateway passes on from user-service's AuthResponse.
 *
 * user-service sends `{accessToken, refreshToken}`; the gateway lifts the
 * refresh token into its HttpOnly cookie and strips it from the body (D-033),
 * so only the access token arrives here. A body that still carried a refresh
 * token would mean the gateway's translation had been bypassed — worth
 * noticing, not worth failing on, so it is ignored rather than rejected.
 */
function decodeTokenPair(body: unknown): TokenPair {
  if (!body || typeof body !== "object") {
    throw new AuthError("The server returned an unreadable response.");
  }
  const raw = body as Record<string, unknown>;
  const accessToken = raw["accessToken"];

  if (typeof accessToken !== "string") {
    throw new AuthError("The server did not return a usable session.");
  }
  return { accessToken };
}

/**
 * A profile field that may be absent, blank, or not a string at all.
 *
 * Returns undefined rather than an empty string so a view can test one thing
 * — `session.email ? ... : ...` — instead of also guarding against "". The
 * schema marks these required, but a user who has not filled in a phone number
 * still has one stored as empty, and that is absent as far as the UI is
 * concerned.
 */
function optionalText(value: unknown): string | undefined {
  if (typeof value !== "string") return undefined;
  const trimmed = value.trim();
  return trimmed === "" ? undefined : trimmed;
}

function asAccountRole(value: unknown): AccountRole {
  return String(value ?? "STUDENT").toUpperCase() === "ADMIN"
    ? "ADMIN"
    : "STUDENT";
}

/**
 * Fetches the signed-in user's profile through the gateway's prefix proxy.
 *
 * Separate from login because user-service's login response is the token pair
 * and nothing else — `AuthResponse` has exactly two fields. Who you are lives
 * behind `GET /api/v1/users/{uid}`, and the uid comes from the token's `sub`.
 *
 * Returns null rather than throwing on any failure. A profile we could not
 * read is a degraded display, not a failed login: the tokens are already valid
 * and the user is already authenticated.
 */
async function fetchProfile(
  uid: string,
  accessToken: string,
): Promise<Record<string, unknown> | null> {
  try {
    const response = await send({
      baseUrl: config.gatewayBaseUrl,
      path: `${ROUTES.profile}/${encodeURIComponent(uid)}`,
      accessToken,
    });
    if (!response.ok || !response.body || typeof response.body !== "object") {
      return null;
    }
    return response.body as Record<string, unknown>;
  } catch {
    return null;
  }
}

/**
 * Turns a verified token pair into a session.
 *
 * `fallbackUsername` is what the user typed at the login form. It is used only
 * when the profile call could not answer, so the avatar and greeting have
 * something truthful-ish to show rather than an empty chip.
 *
 * `email` and `contact` come from the profile call, not from the token: no
 * claim carries either. They stay undefined when that call could not answer,
 * which is why both are optional on Session.
 */
async function sessionFromTokens(
  tokens: TokenPair,
  fallbackUsername: string,
): Promise<AuthResult> {
  const claims = readAccessTokenClaims(tokens.accessToken);
  const userId = claims.subject ?? "";

  const profile = userId ? await fetchProfile(userId, tokens.accessToken) : null;
  const username = String(profile?.["username"] ?? fallbackUsername);

  return {
    session: {
      userId: String(profile?.["uid"] ?? userId),
      username,
      // F1.4.1 — both are on every profile response now, RestrictedUserResponse
      // included, so an ordinary STUDENT viewing their own account gets them.
      // `phone_num` is user-service's name for what the backlog calls contact
      // information; the rename happens here so no view has to know it.
      email: optionalText(profile?.["email"]),
      contact: optionalText(profile?.["phone_num"]),
      // The role the GATEWAY will act on is the one in the token, so show
      // that rather than the profile's column — if they ever disagree, the
      // claim is what governs every authorization decision downstream.
      // The fallback is now unreachable for a non-admin caller:
      // RestrictedUserResponse omits `account_role` entirely.
      role: asAccountRole(claims.role ?? profile?.["account_role"]),
      initials: initialsOf(username),
    },
    tokens,
  };
}

/**
 * Shown wherever the OTP flow is reached against the real stack. One constant
 * so the wording cannot drift between the three entry points.
 */
const OTP_UNAVAILABLE =
  "Registration is not available yet — user-service does not implement the " +
  "email verification step. Ask an admin to create your account.";


async function post(path: string, body: unknown) {
  try {
    return await send({
      baseUrl: config.gatewayBaseUrl,
      path,
      method: "POST",
      body,
    });
  } catch (err) {
    if (err instanceof NetworkError) throw new AuthError(err.message);
    throw err;
  }
}

/** D-027: credentials go to the gateway, which rewrites onto user-service. */
/**
 * Rebuilds the session after a page load, or returns null when there is none.
 *
 * This is the "no access token in memory" case of D-033, not a blanket
 * refresh on every load: the closure is empty because the page is new, and the
 * app cannot render anything until it knows who the user is. The refresh
 * cookie is what answers that — the browser attaches it, the gateway turns it
 * into the body user-service wants, and a fresh access token comes back.
 *
 * Returns null rather than throwing for the ordinary case of a visitor who is
 * simply not signed in: no cookie, an expired one, or one already spent at
 * logout. That is the login page, not an error.
 */
export async function restoreSessionViaGateway(): Promise<AuthResult | null> {
  let tokens: TokenPair;
  try {
    tokens = await refreshAccessTokenViaGateway();
  } catch {
    return null;
  }
  // No typed identifier to fall back on here — the profile call is the only
  // source for the username, and it has the access token it needs.
  return sessionFromTokens(tokens, "");
}

export async function logInViaGateway(
  identifier: string,
  password: string,
): Promise<AuthResult> {
  if (!identifier.trim()) throw new AuthError("Enter your username or NUS email.");
  if (!password) throw new AuthError("Enter your password.");

  // F1.2.1 — one field, either kind of identifier. The server decides which.
  const trimmed = identifier.trim();
  const response = await post(ROUTES.login, { identifier: trimmed, password });
  if (!response.ok) throw decodeAuthError(response.body, response.status);

  // Two round trips, because user-service's login answers with tokens only.
  const fallback = trimmed.includes("@") ? usernameFromEmail(trimmed) : trimmed;
  return sessionFromTokens(decodeTokenPair(response.body), fallback);
}

/**
 * F1.1 + F1.1.2.3 — opens a registration; the OTP step completes it.
 *
 * BLOCKED, deliberately, and it does not call anything.
 *
 * user-service has no OTP. Its `POST /api/v1/users/register` takes email,
 * username and password, answers `201` with an empty body, and the account is
 * live immediately — there is no pending registration, no code, no handle,
 * and no `verify`/`resend` endpoint behind D-027's four auth routes.
 *
 * Calling it anyway would be worse than refusing: the account would really be
 * created, then `decodePending` would fail for want of a handle, and the user
 * would be told registration failed while holding an account they can log into
 * — with no way to discover that. So this stops before the request.
 *
 * F1.1.2.3-F1.1.2.7 are user-service's to build: the code and its five-minute
 * expiry, the three-per-ten-minutes resend limit and the ten-minute block are
 * all server-side rules, and enforcing them in the browser would make them
 * bypassable (frontend/AGENTS.md). The two gateway routes are a small change
 * here once their spec names the endpoints. Until then the fixture client
 * carries this flow, badged as a mock.
 */
export async function signUpViaGateway(
  email: string,
  username: string,
  password: string,
  _contact: string,
): Promise<PendingRegistration> {
  const problem = validateRegistration({ email, username, password });
  if (problem) throw new AuthError(problem);

  throw new AuthError(OTP_UNAVAILABLE);
}

/**
 * F1.1.2.7 — registration completes only on the correct OTP.
 *
 * BLOCKED for the same reason as signUpViaGateway: there is no endpoint behind
 * it. Unreachable in practice, since registration never gets this far.
 */
export async function verifyRegistrationViaGateway(
  _pendingRegistration: PendingRegistration,
  code: string,
): Promise<AuthResult> {
  const problem = validateOtp(code);
  if (problem) throw new AuthError(problem);
  throw new AuthError(OTP_UNAVAILABLE);
}

/**
 * F1.1.2.4 — a replacement code. The server applies the F1.1.2.6 limits.
 *
 * BLOCKED for the same reason as signUpViaGateway.
 */
export async function resendOtpViaGateway(
  _pendingRegistration: PendingRegistration,
): Promise<PendingRegistration> {
  throw new AuthError(OTP_UNAVAILABLE);
}

/**
 * D-015: the refresh exchange goes through the gateway, not direct to
 * user-service.
 *
 * Returns the whole PAIR, not just the access token. user-service rotates the
 * refresh token on every exchange — its `POST /api/v1/users/refresh` responds
 * with an `AuthResponse` carrying both fields — and it detects reuse of a
 * spent one, answering 401 `ErrSessionCompromised`. Keeping the old refresh
 * token would therefore not merely be stale: presenting it again looks like a
 * stolen-token replay and kills the session. The caller must store both.
 */
export async function refreshAccessTokenViaGateway(): Promise<TokenPair> {
  // No body and no argument. The refresh token is an HttpOnly cookie the
  // browser attaches by itself (D-033); this code cannot read it, and the
  // gateway puts it into the body user-service still requires.
  const response = await send({
    baseUrl: config.gatewayBaseUrl,
    path: ROUTES.refresh,
    method: "POST",
  });

  if (!response.ok) throw new AuthError("Your session has expired.");

  const raw = (response.body ?? {}) as Record<string, unknown>;
  const accessToken = raw["accessToken"];
  if (typeof accessToken !== "string") {
    throw new AuthError("The server did not return a new access token.");
  }
  // The rotated refresh token is deliberately NOT here — the gateway strips
  // it from the body and replaces its cookie instead.
  return { accessToken };
}

/**
 * D-014 logout: user-service drops the refresh token from the User DB and
 * blocklists the access token's jti in Redis. The UI cannot do either — it can
 * only ask, then forget its own copies regardless of the answer.
 *
 * user-service still revokes one token of each kind, and they still arrive by
 * different routes — the access token in the Authorization header, whose
 * `jti` it blocklists, and the refresh token in the body, whose session row it
 * deletes. `LogoutRequest` still makes `refreshToken` required. What changed
 * in D-033 is who supplies it: this function sends NO body, and the gateway
 * injects the token from its cookie and then clears it. That is also why the
 * cookie's Path is /auth and not /auth/refresh.
 */
export async function logOutViaGateway(accessToken: string): Promise<void> {
  try {
    await send({
      baseUrl: config.gatewayBaseUrl,
      path: ROUTES.logout,
      method: "POST",
      accessToken,
    });
  } catch {
    // A failed logout call still clears the client. The server-side token
    // stays live until it expires (D-025a), which is not a reason to keep the
    // user signed in here.
  }
}
