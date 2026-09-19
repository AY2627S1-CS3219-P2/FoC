// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-19
// Scope: Auth client — fixture and gateway implementations side by side.
//   2026-09-20: login now takes an identifier (F1.2.1), registration returns a
//   pending registration rather than a session (F1.1.2.7), and OTP verify and
//   resend were added (F1.1.2.4-F1.1.2.6).
// Author review: PENDING — <reviewer to complete>

import { config } from "../../lib/config";
import { NetworkError, send } from "../../lib/http";
import { mockDelay } from "../../lib/mock";
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
    tokens: {
      accessToken: "mock-access-token",
      refreshToken: "mock-refresh-token",
    },
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
export async function refreshAccessToken(): Promise<string> {
  return mockDelay("mock-access-token");
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

function decodeAuthResult(body: unknown): AuthResult {
  if (!body || typeof body !== "object") {
    throw new AuthError("The server returned an unreadable response.");
  }
  const raw = body as Record<string, unknown>;
  const accessToken = raw["accessToken"];
  const refreshToken = raw["refreshToken"];

  if (typeof accessToken !== "string" || typeof refreshToken !== "string") {
    throw new AuthError("The server did not return a usable session.");
  }

  const user = raw["user"];
  const profile = (user && typeof user === "object" ? user : {}) as Record<
    string,
    unknown
  >;
  const email = String(profile["email"] ?? "");
  const username = String(profile["username"] ?? usernameFromEmail(email));
  const role: AccountRole =
    String(profile["role"] ?? "STUDENT").toUpperCase() === "ADMIN"
      ? "ADMIN"
      : "STUDENT";

  return {
    session: {
      userId: String(profile["id"] ?? profile["uid"] ?? ""),
      username,
      email,
      contact: String(profile["contact"] ?? profile["phoneNum"] ?? ""),
      role,
      initials: initialsOf(username),
    },
    tokens: { accessToken, refreshToken },
  };
}

function decodePending(body: unknown, fallbackEmail: string): PendingRegistration {
  const raw = (body ?? {}) as Record<string, unknown>;
  const handle = raw["handle"] ?? raw["registrationId"];
  if (typeof handle !== "string" || !handle) {
    throw new AuthError("The server did not return a registration to verify.");
  }
  const expiresAt = Number(raw["expiresAt"]);
  return {
    handle,
    email: String(raw["email"] ?? fallbackEmail),
    // F1.1.2.5 — five minutes, unless the server says otherwise.
    expiresAt: Number.isFinite(expiresAt) ? expiresAt : Date.now() + 5 * 60_000,
    resendsRemaining: Number(raw["resendsRemaining"] ?? 3),
    blockedUntil:
      raw["blockedUntil"] == null ? null : Number(raw["blockedUntil"]),
  };
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

/** D-027: credentials go to the gateway, which rewrites onto user-service. */
export async function logInViaGateway(
  identifier: string,
  password: string,
): Promise<AuthResult> {
  if (!identifier.trim()) throw new AuthError("Enter your username or NUS email.");
  if (!password) throw new AuthError("Enter your password.");

  // F1.2.1 — one field, either kind of identifier. The server decides which.
  const response = await post(ROUTES.login, {
    identifier: identifier.trim(),
    password,
  });
  if (!response.ok) throw decodeAuthError(response.body, response.status);
  return decodeAuthResult(response.body);
}

/** F1.1 + F1.1.2.3 — opens a registration; the OTP step completes it. */
export async function signUpViaGateway(
  email: string,
  username: string,
  password: string,
  contact: string,
): Promise<PendingRegistration> {
  const problem = validateRegistration({ email, username, password });
  if (problem) throw new AuthError(problem);

  const response = await post(ROUTES.register, {
    email: email.trim(),
    username: username.trim(),
    password,
    contact: contact.trim(),
  });
  if (!response.ok) throw decodeAuthError(response.body, response.status);
  return decodePending(response.body, email.trim());
}

/** F1.1.2.7 — registration completes only on the correct OTP. */
export async function verifyRegistrationViaGateway(
  pendingRegistration: PendingRegistration,
  code: string,
): Promise<AuthResult> {
  const problem = validateOtp(code);
  if (problem) throw new AuthError(problem);

  const response = await post(ROUTES.verify, {
    handle: pendingRegistration.handle,
    code: code.trim(),
  });
  if (!response.ok) throw decodeAuthError(response.body, response.status);
  return decodeAuthResult(response.body);
}

/** F1.1.2.4 — a replacement code. The server applies the F1.1.2.6 limits. */
export async function resendOtpViaGateway(
  pendingRegistration: PendingRegistration,
): Promise<PendingRegistration> {
  const response = await post(ROUTES.resend, {
    handle: pendingRegistration.handle,
  });
  if (!response.ok) throw decodeAuthError(response.body, response.status);
  return decodePending(response.body, pendingRegistration.email);
}

/**
 * D-015: the refresh exchange goes through the gateway, not direct to
 * user-service. Returns the fresh access token only — see TokenStore for why
 * the refresh token is left alone.
 */
export async function refreshAccessTokenViaGateway(
  refreshToken: string,
): Promise<string> {
  const response = await send({
    baseUrl: config.gatewayBaseUrl,
    path: ROUTES.refresh,
    method: "POST",
    body: { refreshToken },
  });

  if (!response.ok) throw new AuthError("Your session has expired.");

  const raw = (response.body ?? {}) as Record<string, unknown>;
  const accessToken = raw["accessToken"];
  if (typeof accessToken !== "string") {
    throw new AuthError("The server did not return a new access token.");
  }
  return accessToken;
}

/**
 * D-014 logout: user-service drops the refresh token from the User DB and
 * blocklists the access token's jti in Redis. The UI cannot do either — it can
 * only ask, then forget its own copies regardless of the answer.
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
