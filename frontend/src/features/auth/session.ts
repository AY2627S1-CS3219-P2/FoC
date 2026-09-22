// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-19
// Scope: Attaches the access token to outgoing requests and performs the
//   refresh exchange recorded in ai/decisions.md D-015.
//   2026-09-21: a refresh now replaces the whole pair, because
//   user-service rotates the refresh token.
//   2026-09-22: comment only — corrected a claim that the gateway's 401
//   catches logout and suspension, which D-024 makes false.
// Author review: PENDING — <reviewer to complete>

import { send, type HttpResponse, type SendOptions } from "../../lib/http";
import type { TokenPair, TokenStore } from "../../lib/tokens";

/**
 * Steps 2 and 3 of the recorded token lifecycle, in one place:
 *
 *   2. The UI attaches the access token to every request (D-011).
 *   3. When the access token has expired, the UI exchanges the refresh token
 *      for a fresh one — through the gateway, not direct to user-service
 *      (D-015) — and retries.
 *
 * "The UI detects an expired access token" is implemented as *the gateway said
 * 401*, not as the client reading `exp` itself. Reading `exp` client-side means
 * trusting an unverified token and racing clock skew; the gateway is the thing
 * that actually decides. The cost is one extra round trip on expiry, which is
 * the cheap direction to err.
 *
 * CORRECTED 2026-09-22: this comment used to claim the gateway's 401 also
 * catches D-014's revocation cases (logout, suspension). It does not. Under
 * D-024 the gateway is not a Redis client and reads no blocklist, so an
 * access token that has been logged out or suspended stays valid to it until
 * `exp` — up to 15 minutes (D-025a). Revocation bites at REFRESH, where
 * user-service checks PostgreSQL: a killed session cannot be renewed, but the
 * access token already in this browser keeps working. So the retry below
 * recovers from expiry, and from nothing else.
 *
 * NOT RECORDED: that the gateway signals an expired token with 401 is an
 * interface detail nobody has written down (ai/decisions.md, Open table).
 * It is isolated to `isUnauthorized` below.
 */

/**
 * Exchanges a refresh token for a fresh PAIR. Supplied by authApi.
 *
 * A pair rather than a lone access token because user-service rotates the
 * refresh token on every exchange and treats a replayed one as a compromised
 * session (`ErrSessionCompromised`). Whatever comes back must replace both.
 */
export type RefreshExchange = (refreshToken: string) => Promise<TokenPair>;

/** Same shape as lib/http's send, minus the token the wrapper supplies. */
export type AuthorizedSend = (
  options: Omit<SendOptions, "accessToken">,
) => Promise<HttpResponse>;

export interface AuthorizedSendDeps {
  tokens: TokenStore;
  refresh: RefreshExchange;
  /** Called when the refresh fails — the session is over and the UI must react. */
  onSessionExpired: () => void;
}

function isUnauthorized(response: HttpResponse): boolean {
  return response.status === 401;
}

export function createAuthorizedSend({
  tokens,
  refresh,
  onSessionExpired,
}: AuthorizedSendDeps): AuthorizedSend {
  // One shared in-flight refresh. Without this, a screen firing three requests
  // at once on a stale token would run three refreshes, and the last two could
  // present an already-rotated token.
  let inFlight: Promise<string | null> | null = null;

  async function refreshOnce(): Promise<string | null> {
    if (inFlight) return inFlight;

    inFlight = (async () => {
      const refreshToken = tokens.getRefreshToken();
      if (!refreshToken) return null;
      try {
        const pair = await refresh(refreshToken);
        // setPair, not setAccessToken: the refresh token we just spent is
        // dead, and presenting it again would read as a replay.
        tokens.setPair(pair);
        return pair.accessToken;
      } catch {
        // A refresh token that no longer verifies against the User DB (D-012)
        // is the end of the session. Nothing to retry.
        tokens.clear();
        onSessionExpired();
        return null;
      } finally {
        inFlight = null;
      }
    })();

    return inFlight;
  }

  return async function authorizedSend(options) {
    const response = await send({
      ...options,
      accessToken: tokens.getAccessToken(),
    });

    if (!isUnauthorized(response)) return response;

    const accessToken = await refreshOnce();
    if (!accessToken) return response;

    // Retried exactly once. A second 401 is a real authorization failure —
    // the caller renders it rather than looping.
    return send({ ...options, accessToken });
  };
}
