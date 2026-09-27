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
 * The gateway has no OTP endpoints, so signUpViaGateway,
 * verifyRegistrationViaGateway and resendOtpViaGateway refuse before sending
 * anything.
 */

/** Gateway paths used by the gateway client. */
const ROUTES = {
  login: "/auth/login",
  register: "/auth/register",
  refresh: "/auth/refresh",
  logout: "/auth/logout",
  /** Placeholder for F1.1.2.7: no endpoint exists and nothing calls it. */
  verify: "/auth/register/verify",
  /** Placeholder for F1.1.2.4: no endpoint exists and nothing calls it. */
  resend: "/auth/register/resend",
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

/** Fixture: completes registration against the issued code (F1.1.2.7). */
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

function decodeAuthError(body: unknown, status: number): AuthError {
  // Use the server's own message when the body has an `error` field.
  const message =
    body && typeof body === "object" && "error" in body
      ? String((body as { error: unknown }).error)
      : null;

  // F1.2.4: a suspended account gets its own error so the UI can name it.
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

/**
 * F1.1 and F1.1.2.3: opens a registration for the OTP step to complete.
 *
 * Refuses before sending anything. The gateway has no OTP endpoints, and
 * user-service's register creates a live account with no verification step,
 * so calling it here would skip F1.1.2.7.
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
 * F1.1.2.7: completes registration on the correct OTP. Always throws after
 * validating the code: there is no endpoint for it.
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
 * F1.1.2.4: requests a replacement code. Always throws: there is no endpoint
 * for it.
 */
export async function resendOtpViaGateway(
  _pendingRegistration: PendingRegistration,
): Promise<PendingRegistration> {
  throw new AuthError(OTP_UNAVAILABLE);
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
export async function logOutViaGateway(accessToken: string): Promise<void> {
  try {
    await send({
      baseUrl: config.gatewayBaseUrl,
      path: ROUTES.logout,
      method: "POST",
      accessToken,
    });
  } catch {
    // A network failure is ignored too; the caller clears its state either way.
  }
}
