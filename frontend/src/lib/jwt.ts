// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-21
// Scope: Reads display claims out of an access token, because user-service's
//   login response carries no profile.
// Author review: Nigeltzy - Used to create the code based on our team's requirements and
// system design, seems like a valid implementation of the code, and the changes made are correct based on the updated requirements.

/**
 * Reads the payload of an access token without verifying its signature.
 *
 * For display only. Anyone can write a JWT payload, so nothing in the UI may
 * treat these claims as a permission; only the server verifies the token.
 * Login and refresh responses carry no profile, so `sub` is also what the UI
 * uses to request one.
 */

/** The claims the UI reads from an access token. Either may be null. */
export interface AccessTokenClaims {
  /** `sub` — the user's UUID. */
  subject: string | null;
  /** `role` — STUDENT or ADMIN. */
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
