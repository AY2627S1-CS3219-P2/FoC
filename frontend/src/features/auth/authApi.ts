// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-19
// Scope: Auth client. Keeps the existing fixture stand-in for user-service and
//   adds a gateway-backed client implementing ai/decisions.md D-010..D-015.
// Author review: PENDING — <reviewer to complete>

import { config } from "../../lib/config";
import { NetworkError, send } from "../../lib/http";
import { mockDelay } from "../../lib/mock";
import type { TokenPair } from "../../lib/tokens";
import type { Session } from "./types";

/**
 * Two clients live here, and App.tsx picks one at wiring time — deliberately
 * not a `useMock` flag passed into a single function, which would be the
 * control coupling root AGENTS.md §5 calls out.
 *
 *   - logIn / signUp / logOut / refreshAccessToken   fixtures. Active today.
 *   - logInViaGateway / ...                          the real flow. Inert
 *                                                    until the gateway runs.
 *
 * THE REQUEST AND RESPONSE SHAPES BELOW ARE NOT A CONTRACT.
 *
 * D-010..D-015 record the token *lifecycle* — who issues, who verifies, what
 * the claims are, where the refresh goes. They do NOT record the endpoint
 * paths or the JSON payloads, and ai/decisions.md lists that as open. Under
 * root AGENTS.md §1 an API interface belongs to user-service's owner, spec
 * first (§8). So the constants and decoders here are a placeholder with the
 * right *surface*, to be REPLACED by the generated client — not reconciled
 * with, and never cited as, user-service's spec.
 */

/** UNRECORDED — placeholder paths. See the note above. */
const ROUTES = {
  login: "/auth/login",
  register: "/auth/register",
  refresh: "/auth/refresh",
  logout: "/auth/logout",
} as const;

export class AuthError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "AuthError";
  }
}

/** What a successful login yields: who you are, plus the pair from D-011. */
export interface AuthResult {
  session: Session;
  tokens: TokenPair;
}

function initialsOf(name: string): string {
  return name
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((part) => part[0]?.toUpperCase() ?? "")
    .join("");
}

function nameFromEmail(email: string): string {
  const local = email.split("@")[0] ?? "student";
  return local
    .split(/[._-]+/)
    .filter(Boolean)
    .map((part) => part[0]!.toUpperCase() + part.slice(1))
    .join(" ");
}

// ---------------------------------------------------------------------------
// Fixtures — active while user-service does not exist
// ---------------------------------------------------------------------------

function fixtureResult(email: string, name: string): AuthResult {
  return {
    session: {
      userId: "mock-user-1",
      name,
      initials: initialsOf(name) || "NU",
      email,
    },
    tokens: {
      accessToken: "mock-access-token",
      refreshToken: "mock-refresh-token",
    },
  };
}

/**
 * MOCK. Authenticates nothing. Accepts any NUS-looking email, ignores the
 * password entirely, and hands back a fixture session with obviously-fake
 * tokens so the shell has a user to render and the token plumbing has
 * something to carry. No credential is transmitted, stored or checked.
 */
export async function logIn(email: string): Promise<AuthResult> {
  if (!email.trim()) throw new AuthError("Enter your NUS email.");
  return mockDelay(fixtureResult(email, nameFromEmail(email)));
}

/** MOCK. See logIn. */
export async function signUp(email: string, name: string): Promise<AuthResult> {
  if (!email.trim()) throw new AuthError("Enter your NUS email.");
  if (!name.trim()) throw new AuthError("Enter your name.");
  return mockDelay(fixtureResult(email, name));
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
// Gateway-backed — inert until api-gateway runs and user-service exists
// ---------------------------------------------------------------------------

function decodeAuthError(body: unknown, status: number): AuthError {
  // Not assuming a shared error envelope (frontend/AGENTS.md, Gotchas):
  // supplier-service returns {"error": "..."}, and whether user-service and
  // the gateway match is their owners' decision. Read it if it is there.
  if (body && typeof body === "object" && "error" in body) {
    return new AuthError(String((body as { error: unknown }).error));
  }
  if (status === 401) return new AuthError("Incorrect email or password.");
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
  const name = String(profile["name"] ?? nameFromEmail(email));

  return {
    session: {
      userId: String(profile["id"] ?? ""),
      name,
      initials: initialsOf(name) || "NU",
      email,
    },
    tokens: { accessToken, refreshToken },
  };
}

/** Step 1 of D-010: credentials go to the gateway, which forwards them on. */
export async function logInViaGateway(
  email: string,
  password: string,
): Promise<AuthResult> {
  if (!email.trim()) throw new AuthError("Enter your NUS email.");
  if (!password) throw new AuthError("Enter your password.");

  let response;
  try {
    response = await send({
      baseUrl: config.gatewayBaseUrl,
      path: ROUTES.login,
      method: "POST",
      body: { email, password },
    });
  } catch (err) {
    if (err instanceof NetworkError) throw new AuthError(err.message);
    throw err;
  }

  if (!response.ok) throw decodeAuthError(response.body, response.status);
  return decodeAuthResult(response.body);
}

/** Registration, which issues a session the same way login does (D-012). */
export async function signUpViaGateway(
  email: string,
  name: string,
  password: string,
): Promise<AuthResult> {
  if (!email.trim()) throw new AuthError("Enter your NUS email.");
  if (!name.trim()) throw new AuthError("Enter your name.");
  if (!password) throw new AuthError("Choose a password.");

  let response;
  try {
    response = await send({
      baseUrl: config.gatewayBaseUrl,
      path: ROUTES.register,
      method: "POST",
      body: { email, name, password },
    });
  } catch (err) {
    if (err instanceof NetworkError) throw new AuthError(err.message);
    throw err;
  }

  if (!response.ok) throw decodeAuthError(response.body, response.status);
  return decodeAuthResult(response.body);
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
    // stays live until it expires, which is the gateway's problem to surface,
    // not a reason to keep the user signed in here.
  }
}
