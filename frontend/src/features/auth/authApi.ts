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
//   2026-10-10 (Claude Code, Opus 5.5): the three OTP calls now reach
//   user-service's OTP endpoints (PR #14) through the gateway's D-041 routes;
//   verify no longer signs in (LoginPage logs in afterwards), an unverified
//   login is its own error, and a rate-limited code request carries when the
//   block ends.
// Author review: Nigeltzy - The AI was used to generate the original boilerplate code,
// as per our team's decisions, direction and requirements. As far as I can tell, the code 
// is valid and I have asked the AI to explain some aprts as well as include comments for my
// understanding. There are parts of the code that were originally mocked and now updated for srvices.

import { config } from "../../lib/config";
import { NetworkError, send } from "../../lib/http";
import { mockDelay } from "../../lib/mock";
import { readAccessTokenClaims } from "../../lib/jwt";
import type { TokenPair } from "../../lib/tokens";
import * as fixtureOtp from "./fixtureOtp";
import { underRefreshLock } from "./session";
import type { AccountRole, PendingRegistration, Session } from "./types";
import { validateOtp, validateRegistration } from "./validation";

/**
 * Two auth clients with the same surface. App.tsx picks one at startup:
 *
 *   - logIn / signUp / verifyRegistration / ...   in-browser fixtures, used
 *                                                 only when VITE_USE_FIXTURES=true.
 *   - logInViaGateway / ...                       the gateway client, used
 *                                                 otherwise.
 *
 * Verifying a code only activates the account: neither client returns a
 * session from it, and LoginPage logs in afterwards with the credentials it
 * already holds.
 */

/** Gateway paths used by the gateway client. */
const ROUTES = {
  login: "/auth/login",
  register: "/auth/register",
  refresh: "/auth/refresh",
  logout: "/auth/logout",
  /** F1.1.2.7 — D-041, onto user-service's /api/v1/users/verify-otp. */
  verify: "/auth/verify-otp",
  /** F1.1.2.4 — D-041, onto user-service's /api/v1/users/resend-otp. */
  resend: "/auth/resend-otp",
  /** The profile route; `/{uid}` is appended. */
  profile: "/api/users",
} as const;

export class AuthError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "AuthError";
  }
}

/**
 * F1.2.4: a suspended account is told it is suspended, a different outcome
 * from bad credentials (F1.2.3). LoginPage checks for this type to show it.
 */
export class SuspendedAccountError extends AuthError {
  constructor(message = "This account is suspended. Contact an administrator.") {
    super(message);
    this.name = "SuspendedAccountError";
  }
}

// AI-generated (edited by nigeltzy).
/**
 * user-service refuses to log in an account whose email is not verified yet.
 * LoginPage checks for this type to send the user to the OTP step instead.
 */
export class UnverifiedAccountError extends AuthError {
  constructor(message = "Verify your email before logging in.") {
    super(message);
    this.name = "UnverifiedAccountError";
  }
}

// AI-generated (edited by nigeltzy).
/**
 * F1.1.2.6: too many code requests. `blockedUntil` (epoch ms) is when the OTP
 * step may offer a new code again, so it can disable the button until then.
 */
export class OtpBlockedError extends AuthError {
  constructor(
    readonly blockedUntil: number,
    message = "Too many code requests. Try again once the timer runs out.",
  ) {
    super(message);
    this.name = "OtpBlockedError";
  }
}

/** What a successful login yields: the session and the access token. */
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
// Fixture client, used only when VITE_USE_FIXTURES=true (lib/config.ts).
// Otherwise Vite leaves these functions out of the bundle.
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
 * Fixture login. Accepts any non-empty username or NUS email (F1.2.1), never
 * sees the password, and returns a fixture session with a fake access token.
 * Nothing is sent to a server.
 */
export async function logIn(identifier: string): Promise<AuthResult> {
  const trimmed = identifier.trim();
  if (!trimmed) throw new AuthError("Enter your username or NUS email.");

  // The identifier "suspended" returns the suspended-account error (F1.2.4),
  // so that screen can be seen with fixtures.
  if (trimmed.toLowerCase() === "suspended") {
    throw new SuspendedAccountError();
  }

  const isEmail = trimmed.includes("@");
  const email = isEmail ? trimmed : `${trimmed}@u.nus.edu`;
  const username = isEmail ? usernameFromEmail(trimmed) : trimmed;
  return mockDelay(fixtureResult(fixtureSession(email, username)));
}

/**
 * Fixture signup (F1.1.2.7). Returns a pending registration, not a session;
 * only verifyRegistration completes it.
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

/**
 * Fixture: completes registration against the issued code (F1.1.2.7). Like the
 * gateway client it returns no session; LoginPage logs in afterwards.
 */
export async function verifyRegistration(
  pendingRegistration: PendingRegistration,
  code: string,
): Promise<void> {
  const problem = validateOtp(code);
  if (problem) throw new AuthError(problem);
  fixtureOtp.verify(pendingRegistration.handle, code);
  return mockDelay(undefined);
}

/**
 * Fixture: never reached. Fixture logins are never unverified, so LoginPage
 * has no reason to call it.
 */
export async function resumeVerification(
  _email: string,
): Promise<PendingRegistration> {
  throw new AuthError("Fixture accounts are always verified.");
}

/** Fixture: issues a replacement code with the resend limits (F1.1.2.4-F1.1.2.6). */
export async function resendOtp(
  pendingRegistration: PendingRegistration,
): Promise<PendingRegistration> {
  return mockDelay(fixtureOtp.resend(pendingRegistration.handle));
}

/** Fixture logout. The tokens are fake, so there is nothing to revoke. */
export async function logOut(): Promise<void> {
  return mockDelay(undefined);
}

/** Fixture refresh: returns a new fake access token. */
export async function refreshAccessToken(): Promise<TokenPair> {
  return mockDelay({ accessToken: "mock-access-token" });
}

// ---------------------------------------------------------------------------
// Gateway client, used unless VITE_USE_FIXTURES=true
//
// The gateway keeps the refresh token in an HttpOnly cookie, so nothing here
// reads or sends one.
// ---------------------------------------------------------------------------

/** user-service's `error` text for a suspended account's login. */
const SUSPENDED_ERROR = "account suspended";

/** user-service's `error` text for an unverified account's login. */
const UNVERIFIED_ERROR = "ACCOUNT_UNVERIFIED";

function decodeAuthError(body: unknown, status: number): AuthError {
  // Use the server's own message when the body has an `error` field.
  const message =
    body && typeof body === "object" && "error" in body
      ? String((body as { error: unknown }).error)
      : null;

  // AI-generated (edited by nigeltzy).
  // F1.2.4: user-service answers a suspended account's login with 403 and
  // SUSPENDED_ERROR. The UI names it with its own sentence, not the raw text.
  if (status === 403 && message === SUSPENDED_ERROR) {
    return new SuspendedAccountError();
  }
  // AI-generated (edited by nigeltzy).
  // The 403 user-service sends while the account's email is unverified.
  if (status === 403 && message === UNVERIFIED_ERROR) {
    return new UnverifiedAccountError();
  }
  if (message) return new AuthError(message);
  // F1.2.3 — a generic error for bad credentials, naming neither field.
  if (status === 401) return new AuthError("Those details did not match.");
  return new AuthError(`Sign-in failed (${status}).`);
}

/**
 * Reads the access token from a login response. The gateway moves the refresh
 * token into its cookie before the body arrives, so a refreshToken field here
 * is ignored.
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
 * A profile string, trimmed, or undefined when it is missing or blank. A user
 * with no phone number comes back with "", which the UI treats as absent.
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
 * Fetches a user's profile through the gateway. The login response carries
 * only tokens, so this is where username, email and contact come from.
 *
 * Returns null on any failure: the tokens are already valid, so a missing
 * profile degrades the display rather than failing the login.
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
 * Builds a session from an access token plus the profile it points at.
 *
 * `fallbackUsername` is used only when the profile call fails: the typed
 * identifier at login, or "" on a page-load restore. `email` and `contact`
 * come only from the profile, so they stay undefined when it fails.
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
      // F1.4.1: user-service calls the contact field `phone_num`; it is renamed
      // here so no view needs to know.
      email: optionalText(profile?.["email"]),
      contact: optionalText(profile?.["phone_num"]),
      // Prefer the token's role claim: it is what the gateway acts on. The
      // profile's account_role is only a fallback, and non-admin profiles
      // omit it.
      role: asAccountRole(claims.role ?? profile?.["account_role"]),
      initials: initialsOf(username),
    },
    tokens,
  };
}

// AI-generated (edited by nigeltzy).
// user-service's OTP rules, from the "OTP Feature" section of
// user-service/AGENTS.md (PR #14). The server enforces them; these only let
// the OTP step show the same numbers.

/** A code expires five minutes after it is issued. */
const OTP_TTL_MS = 5 * 60 * 1000;

/**
 * Code requests allowed in the window. The first code counts, so a new
 * registration has OTP_REQUEST_LIMIT - 1 replacements left.
 */
const OTP_REQUEST_LIMIT = 3;

/**
 * How long user-service refuses further requests once over the limit. Each
 * refused request restarts it, which is why the OTP step disables its button
 * for the whole block.
 */
const OTP_BLOCK_MS = 10 * 60 * 1000;

/** The pending registration for a code that was just sent to `email`. */
function pendingFor(
  email: string,
  resendsRemaining: number | null,
): PendingRegistration {
  return {
    // The gateway client keys a registration by its email; nothing else.
    handle: email,
    email,
    expiresAt: Date.now() + OTP_TTL_MS,
    resendsRemaining,
    blockedUntil: null,
  };
}

/** The `error` text of a user-service response, or null when there is none. */
function serverMessage(body: unknown): string | null {
  return body && typeof body === "object" && "error" in body
    ? String((body as { error: unknown }).error)
    : null;
}


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


/**
 * Rebuilds the session from the refresh cookie after a page load, or returns
 * null when there is no live session: no cookie, an expired one, or one spent
 * at logout. Null means the login page, not an error.
 */
export async function restoreSessionViaGateway(): Promise<AuthResult | null> {
  let tokens: TokenPair;
  try {
    // AI-generated (edited by nigeltzy).
    // Under the same lock as createAuthorizedSend: a second tab, or StrictMode's
    // second mount, would otherwise present the cookie this one is spending.
    tokens = await underRefreshLock(refreshAccessTokenViaGateway);
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

// AI-generated (edited by nigeltzy).
/**
 * F1.1 and F1.1.2.3: creates the account, unverified, and has user-service
 * email the first code. The account cannot log in until verified (F1.1.2.7).
 *
 * The contact number is not sent: user-service's register takes no phone
 * number.
 */
export async function signUpViaGateway(
  email: string,
  username: string,
  password: string,
  _contact: string,
): Promise<PendingRegistration> {
  const problem = validateRegistration({ email, username, password });
  if (problem) throw new AuthError(problem);

  const trimmedEmail = email.trim();
  const response = await post(ROUTES.register, {
    email: trimmedEmail,
    username: username.trim(),
    password,
  });
  if (response.ok) return pendingFor(trimmedEmail, OTP_REQUEST_LIMIT - 1);

  if (response.status === 409) {
    throw new AuthError(
      "That email or username is already taken. If it is your account and " +
        "you never verified it, log in with your NUS email to get a new code.",
    );
  }
  if (response.status === 429) {
    throw new AuthError(
      "Too many code requests for this email. Try again in 10 minutes.",
    );
  }
  if (response.status >= 500) {
    // user-service keeps the account when only the email failed to send, so
    // logging in is how the user reaches a new code.
    throw new AuthError(
      "We could not send your code. Log in with your NUS email to request a " +
        "new one; if those details are not recognised, sign up again.",
    );
  }
  throw new AuthError(
    serverMessage(response.body) ?? `Sign-up failed (${response.status}).`,
  );
}

// AI-generated (edited by nigeltzy).
/**
 * F1.1.2.7: activates the account on the correct code. Returns no session —
 * user-service issues no tokens here — so the caller logs in afterwards.
 *
 * An account that is already verified (409) counts as success. That is rare:
 * user-service deletes a used code, so a reused one usually gets 404, which
 * is why LoginPage never verifies twice.
 */
export async function verifyRegistrationViaGateway(
  pendingRegistration: PendingRegistration,
  code: string,
): Promise<void> {
  const problem = validateOtp(code);
  if (problem) throw new AuthError(problem);

  const response = await post(ROUTES.verify, {
    email: pendingRegistration.email,
    otp: code.trim(),
  });
  if (response.ok || response.status === 409) return;

  switch (response.status) {
    case 400:
      throw new AuthError("That code is not right. Check it and try again.");
    case 404:
      throw new AuthError("That code has expired. Ask for a new one.");
    case 429:
      throw new OtpBlockedError(Date.now() + OTP_BLOCK_MS);
    default:
      throw new AuthError(
        serverMessage(response.body) ??
          `Could not verify the code (${response.status}).`,
      );
  }
}

// AI-generated (edited by nigeltzy).
/** Asks user-service to email a new code to `email`, replacing any earlier one. */
async function requestCode(email: string): Promise<void> {
  const response = await post(ROUTES.resend, { email });
  if (response.ok) return;

  switch (response.status) {
    case 404:
      throw new AuthError("No account uses that email. Sign up first.");
    case 409:
      throw new AuthError("This account is already verified. Log in instead.");
    case 429:
      throw new OtpBlockedError(Date.now() + OTP_BLOCK_MS);
    default:
      throw new AuthError(
        serverMessage(response.body) ??
          `Could not send a new code (${response.status}).`,
      );
  }
}

// AI-generated (edited by nigeltzy).
/**
 * F1.1.2.4: requests a replacement code. F1.1.2.5: the previous code stops
 * working once this returns.
 */
export async function resendOtpViaGateway(
  pendingRegistration: PendingRegistration,
): Promise<PendingRegistration> {
  await requestCode(pendingRegistration.email);
  const left = pendingRegistration.resendsRemaining;
  return pendingFor(
    pendingRegistration.email,
    left === null ? null : Math.max(0, left - 1),
  );
}

// AI-generated (edited by nigeltzy).
/**
 * Opens the OTP step for an account that registered but never verified,
 * reached from a login refused with UnverifiedAccountError. Sends a fresh
 * code, since any earlier one may have expired.
 *
 * How many requests that account has already made is known only to
 * user-service, so the replacement count is left unknown (null).
 */
export async function resumeVerificationViaGateway(
  email: string,
): Promise<PendingRegistration> {
  const trimmed = email.trim();
  await requestCode(trimmed);
  return pendingFor(trimmed, null);
}

/**
 * Exchanges the refresh cookie for a new access token. The browser attaches
 * the cookie and the gateway replaces it with the rotated one, so the caller
 * stores only the returned access token. Throws when the exchange fails.
 */
export async function refreshAccessTokenViaGateway(): Promise<TokenPair> {
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

  return { accessToken };
}

/**
 * Asks the gateway to end the session. Sends the access token and no body;
 * the gateway supplies the refresh token from its cookie. The response is not
 * checked: the caller clears its own state whatever the server answered.
 */
export async function logOutViaGateway(accessToken: string | null): Promise<void> {
  // AI-generated (edited by nigeltzy).
  // Refresh first, under the refresh lock. After 15 idle minutes the token in
  // memory has expired, user-service rejects the logout with 401, and the
  // session is never revoked. If the refresh fails there is no live session to
  // revoke, and the logout below still has the gateway clear the cookie.
  let token = accessToken;
  try {
    token = (await underRefreshLock(refreshAccessTokenViaGateway)).accessToken;
  } catch {
    // Keep the token we have.
  }

  try {
    await send({
      baseUrl: config.gatewayBaseUrl,
      path: ROUTES.logout,
      method: "POST",
      accessToken: token,
    });
  } catch {
    // A network failure is ignored too; the caller clears its state either way.
  }
}
