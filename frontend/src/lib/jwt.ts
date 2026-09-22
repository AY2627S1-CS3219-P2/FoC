// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-21
// Scope: Reads display claims out of an access token, because user-service's
//   login response carries no profile.
// Author review: Nigeltzy - Used to create the code based on our team's requirements and
// system design, seems like a valid implementation of the code, and the changes made are correct based on the updated requirements.

/**
 * Reads the payload of an access token WITHOUT verifying it.
 *
 * THIS IS NOT AUTHENTICATION, and nothing in this folder may treat it as
 * such. A JWT's payload is base64url, not ciphertext: anyone can write one.
 * The only thing that decides whether a token is real is the gateway, which
 * checks the RS256 signature against user-service's JWKS (D-023) and refuses
 * the request if it does not hold.
 *
 * What this is for: user-service's `POST /api/v1/users/login` answers with
 * `{accessToken, refreshToken}` and nothing else — no user object. The `sub`
 * and `role` claims are therefore the only place the UI can learn who it just
 * logged in, and it needs them to address `GET /api/v1/users/{uid}` at all.
 * Reading them to fill an avatar and a nav is safe precisely because none of
 * it is a permission: hiding a button is an affordance, not access control
 * (frontend/AGENTS.md, Gotchas), and every real check happens server-side on
 * the verified token.
 */

/** The subset of D-011's claims the UI reads. Both may be absent. */
export interface AccessTokenClaims {
  /** `sub` — the user's UUID. */
  subject: string | null;
  /** `role` — STUDENT or ADMIN (D-019). */
  role: string | null;
}

function decodePayloadSegment(segment: string): Record<string, unknown> | null {
  try {
    const base64 = segment.replace(/-/g, "+").replace(/_/g, "/");
    const padded = base64 + "=".repeat((4 - (base64.length % 4)) % 4);
    const binary = atob(padded);
    // atob yields one char per byte; the claims may hold non-ASCII, so go
    // back through UTF-8 rather than trusting the raw string.
    const bytes = Uint8Array.from(binary, (char) => char.charCodeAt(0));
    const parsed: unknown = JSON.parse(new TextDecoder().decode(bytes));
    return parsed && typeof parsed === "object"
      ? (parsed as Record<string, unknown>)
      : null;
  } catch {
    // A token we cannot read is not an error worth surfacing: the caller
    // falls back, and the gateway rejects it on the next request anyway.
    return null;
  }
}

/** Pulls `sub` and `role` out of an access token. Never throws. */
export function readAccessTokenClaims(accessToken: string): AccessTokenClaims {
  const segments = accessToken.split(".");
  const payload = segments.length === 3 ? decodePayloadSegment(segments[1]!) : null;
  if (!payload) return { subject: null, role: null };

  const subject = payload["sub"];
  const role = payload["role"];
  return {
    subject: typeof subject === "string" && subject ? subject : null,
    role: typeof role === "string" && role ? role : null,
  };
}
