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
 * Attaches the access token to every request and, when the gateway answers
 * 401, exchanges the refresh cookie for a new access token and retries once.
 * Expiry is detected from the 401 alone (see isUnauthorized); the client
 * never reads `exp`.
 */

/**
 * Exchanges the refresh cookie for a new access token. Supplied by authApi;
 * takes no argument because the browser attaches the cookie.
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
 * The Web Locks name for refreshes made through createAuthorizedSend. The lock
 * is shared by every tab on the origin, so two tabs cannot spend the same
 * refresh cookie at once; user-service treats a reused refresh token as theft
 * and revokes all of that user's sessions.
 */
const REFRESH_LOCK = "foc.auth.refresh";

/**
 * Runs `fn` under the cross-tab refresh lock. Where Web Locks are missing
 * (`navigator.locks` needs a secure context, so plain HTTP other than
 * localhost), it runs `fn` directly, without the cross-tab guarantee.
 */
export async function underRefreshLock<T>(fn: () => Promise<T>): Promise<T> {
  if (typeof navigator === "undefined" || !navigator.locks) return fn();
  return navigator.locks.request(REFRESH_LOCK, fn);
}

export function createAuthorizedSend({
  tokens,
  refresh,
  onSessionExpired,
}: AuthorizedSendDeps): AuthorizedSend {
  // One shared in-flight refresh per page, so requests fired together cause
  // one exchange. The Web Lock covers other tabs.
  let inFlight: Promise<string | null> | null = null;

  async function refreshOnce(): Promise<string | null> {
    if (inFlight) return inFlight;

    inFlight = (async () => {
      try {
        return await underRefreshLock(async () => {
          // Re-read inside the lock: if another refresh in this page stored a
          // token while this one waited, use it instead of spending the cookie.
          const existing = tokens.getAccessToken();
          if (existing) return existing;

          const pair = await refresh();
          tokens.setPair(pair);
          return pair.accessToken;
        });
      } catch {
        // Any failed refresh ends the session, whether the gateway refused the
        // cookie or the request never got through.
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
    // No access token in memory: exchange the refresh cookie before sending.
    let accessToken = tokens.getAccessToken();
    if (!accessToken) {
      accessToken = await refreshOnce();
      if (!accessToken) {
        return { ok: false, status: 401, body: null };
      }
    }

    const response = await send({ ...options, accessToken });
    if (!isUnauthorized(response)) return response;

    // The server refused the in-memory token, usually because it expired.
    // Clear it so refreshOnce does not hand the same token back from inside
    // the lock.
    tokens.clear();
    const renewed = await refreshOnce();
    if (!renewed) return response;

    // Retried exactly once. A second 401 is a real authorization failure —
    // the caller renders it rather than looping.
    return send({ ...options, accessToken: renewed });
  };
}
