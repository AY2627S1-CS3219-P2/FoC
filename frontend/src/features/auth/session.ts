// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-19
// Scope: Attaches the access token to outgoing requests and performs the
//   refresh exchange recorded in ai/decisions.md D-015.
// Author review: PENDING — <reviewer to complete>

import { send, type HttpResponse, type SendOptions } from "../../lib/http";
import type { TokenStore } from "../../lib/tokens";

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
 * that actually decides, and it also catches the revocation cases in D-014
 * (logout blocklist, suspension) where the token is unexpired but dead. The
 * cost is one extra round trip on expiry, which is the cheap direction to err.
 *
 * NOT RECORDED: that the gateway signals an expired/revoked token with 401 is
 * an interface detail nobody has written down (ai/decisions.md, Open table).
 * It is isolated to `isUnauthorized` below.
 */

/** Exchanges a refresh token for a new access token. Supplied by authApi. */
export type RefreshExchange = (refreshToken: string) => Promise<string>;

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
        const accessToken = await refresh(refreshToken);
        tokens.setAccessToken(accessToken);
        return accessToken;
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
