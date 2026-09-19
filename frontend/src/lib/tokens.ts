// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-19
// Scope: Client-side access/refresh token store, implementing the token
//   lifecycle recorded in ai/decisions.md D-011 and D-015.
// Author review: PENDING — <reviewer to complete>

/**
 * Holds the access token (AT) and refresh token (RT) the gateway hands back at
 * login (D-011, D-012), so that lib/http can attach the AT to every request
 * and features/auth/session can exchange the RT when the AT expires (D-015).
 *
 * WHERE THESE ARE KEPT IS NOT DECIDED. The Open table in ai/decisions.md lists
 * "Where the UI keeps the AT and RT" as unanswered, so this deliberately keeps
 * them in a closure and nowhere else:
 *
 *   - Nothing is written to localStorage, sessionStorage or a cookie. Anything
 *     persisted is readable by any script on the origin, and that trade-off is
 *     the team's to make, not this file's.
 *   - The consequence is real and visible: a page reload logs the user out.
 *     That is the honest default until someone records the alternative.
 *
 * Whatever is chosen, it changes only this file — nothing else touches the
 * token values.
 *
 * No module-level `let` holds the tokens: the store is created once in App.tsx
 * and passed down, mirroring "config is injected, never global" (root
 * AGENTS.md §4.4) and avoiding the common coupling §5 lists.
 */

/** The pair user-service issues and the UI carries (D-011). */
export interface TokenPair {
  accessToken: string;
  refreshToken: string;
}

export interface TokenStore {
  /** The current access token, or null when logged out. */
  getAccessToken(): string | null;
  /** The current refresh token, or null when logged out. */
  getRefreshToken(): string | null;
  /** Replaces both, e.g. after login. */
  setPair(pair: TokenPair): void;
  /**
   * Replaces only the access token, after a refresh exchange. The RT is left
   * alone because D-015 does not say the refresh response rotates it — if
   * user-service turns out to rotate RTs, call setPair instead.
   */
  setAccessToken(accessToken: string): void;
  /** Drops both, e.g. on logout or a failed refresh. */
  clear(): void;
}

export function createTokenStore(): TokenStore {
  let accessToken: string | null = null;
  let refreshToken: string | null = null;

  return {
    getAccessToken: () => accessToken,
    getRefreshToken: () => refreshToken,
    setPair(pair) {
      accessToken = pair.accessToken;
      refreshToken = pair.refreshToken;
    },
    setAccessToken(next) {
      accessToken = next;
    },
    clear() {
      accessToken = null;
      refreshToken = null;
    },
  };
}
