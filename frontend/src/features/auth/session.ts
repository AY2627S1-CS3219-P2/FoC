// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-19
// Scope: Attaches the access token to outgoing requests and performs the
//   refresh exchange recorded in ai/decisions.md D-015.
//   2026-09-21: a refresh now replaces the whole pair, because
//   user-service rotates the refresh token.
//   2026-09-22: comment only — corrected a claim that the gateway's 401
//   catches logout and suspension, which D-024 makes false. Later that day:
//   D-033 — the refresh token is a cookie, so the exchange takes no argument,
//   runs under a cross-tab Web Lock, and fires on demand rather than on load.
// Author review: Nigeltzy - Used to create the original boilerplate generation, 
// which seems valid and, then afterwards it was used to update the changes based on the team's discussions, they
// made the updates and updated the code and it seems correct. Includes comments on changes made, reasoning and observations
// supplied by me and the team as well.

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
export type RefreshExchange = () => Promise<TokenPair>;

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

/**
 * The Web Locks name the refresh is serialised under.
 *
 * Locks are scoped to the origin and shared across every tab on it, which is
 * the point: user-service rotates the refresh token on each exchange and
 * treats a replayed one as a stolen session, revoking ALL of that user's
 * sessions. Two tabs waking together would otherwise present the same cookie
 * and sign the user out everywhere (D-033).
 */
const REFRESH_LOCK = "foc.auth.refresh";

/**
 * Runs `fn` under the cross-tab refresh lock, or directly where Web Locks are
 * unavailable.
 *
 * `navigator.locks` needs a secure context, so it is absent over plain HTTP on
 * anything but localhost. Falling back keeps the app working; it just loses
 * the cross-tab guarantee, which is no worse than before this existed.
 */
async function underRefreshLock<T>(fn: () => Promise<T>): Promise<T> {
  if (typeof navigator === "undefined" || !navigator.locks) return fn();
  return navigator.locks.request(REFRESH_LOCK, fn);
}

export function createAuthorizedSend({
  tokens,
  refresh,
  onSessionExpired,
}: AuthorizedSendDeps): AuthorizedSend {
  // One shared in-flight refresh WITHIN this page. The Web Lock covers other
  // tabs; this covers three requests fired by one screen, without making two
  // of them queue on a lock they do not need.
  let inFlight: Promise<string | null> | null = null;

  async function refreshOnce(): Promise<string | null> {
    if (inFlight) return inFlight;

    inFlight = (async () => {
      try {
        return await underRefreshLock(async () => {
          // Re-read inside the lock. Another tab may have refreshed while we
          // waited, in which case the cookie it left is the live one and
          // spending ours would look like a replay.
          const existing = tokens.getAccessToken();
          if (existing) return existing;

          // No argument: the refresh token is an HttpOnly cookie the browser
          // attaches itself (D-033). There is nothing here to pass.
          const pair = await refresh();
          tokens.setPair(pair);
          return pair.accessToken;
        });
      } catch {
        // A refresh the gateway rejects is the end of the session: the cookie
        // is gone, expired, or was already spent. Nothing to retry.
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
    // ON DEMAND, not on page load (D-033). After a reload there is no access
    // token in memory but the cookie is still live, so the first authenticated
    // request exchanges it rather than every page load paying a round trip.
    let accessToken = tokens.getAccessToken();
    if (!accessToken) {
      accessToken = await refreshOnce();
      if (!accessToken) {
        return { ok: false, status: 401, body: null };
      }
    }

    const response = await send({ ...options, accessToken });
    if (!isUnauthorized(response)) return response;

    // The token was live in memory but the server refused it — expired
    // between the read and the send. Clear it so refreshOnce does not return
    // the same dead one from inside the lock.
    tokens.clear();
    const renewed = await refreshOnce();
    if (!renewed) return response;

    // Retried exactly once. A second 401 is a real authorization failure —
    // the caller renders it rather than looping.
    return send({ ...options, accessToken: renewed });
  };
}
