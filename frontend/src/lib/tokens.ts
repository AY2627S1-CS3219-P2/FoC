// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-19
// Scope: Client-side access/refresh token store, implementing the token
//   lifecycle recorded in ai/decisions.md D-011 and D-015.
//   2026-09-21: dropped setAccessToken — user-service rotates RTs.
//   2026-09-22: the refresh token left this file entirely. D-033 puts it in
//   an HttpOnly cookie the gateway owns, which JS cannot read.
// Author review: PENDING — <reviewer to complete>

/**
 * Holds the access token (AT) and refresh token (RT) the gateway hands back at
 * login (D-011, D-012), so that lib/http can attach the AT to every request
 * and features/auth/session can exchange the RT when the AT expires (D-015).
 *
 * SETTLED BY D-033. The rule for the access token is **in memory, never
 * written to storage** — not "a module-level variable", which root AGENTS.md
 * §5 rules out. The closure below already satisfies it and did not change.
 *
 *   - Nothing here is written to localStorage, sessionStorage or a cookie. An
 *     XSS can read this token for the lifetime of the page, and it expires in
 *     15 minutes.
 *   - The REFRESH token is not here at all. It is an HttpOnly cookie the
 *     gateway sets and reads (D-033), so this code cannot see it, and neither
 *     can anything else running on the page.
 *   - A page reload therefore leaves no access token but a live cookie, which
 *     is what makes the session survive: features/auth/session exchanges the
 *     cookie for a fresh token on the first authenticated request.
 *
 * No module-level `let` holds the tokens: the store is created once in App.tsx
 * and passed down, mirroring "config is injected, never global" (root
 * AGENTS.md §4.4) and avoiding the common coupling §5 lists.
 */

/**
 * What the browser receives from an auth route now.
 *
 * user-service still issues a pair (D-011), but the gateway lifts the refresh
 * token into its cookie and strips it from the body (D-033), so only the
 * access token reaches this code.
 */
export interface TokenPair {
  accessToken: string;
}

export interface TokenStore {
  /** The current access token, or null when there is none in memory. */
  getAccessToken(): string | null;
  /**
   * Replaces the access token — after login AND after a refresh exchange.
   *
   * Rotation of the refresh token is no longer this file's problem: the
   * gateway replaces its own cookie on every exchange, so there is no stale
   * copy here to leave behind.
   */
  setPair(pair: TokenPair): void;
  /**
   * Drops the access token.
   *
   * This does NOT end the session — the refresh cookie is the session, and
   * only the gateway can clear it, on logout. Calling this alone just forces
   * the next request to exchange the cookie for a new token.
   */
  clear(): void;
}

export function createTokenStore(): TokenStore {
  let accessToken: string | null = null;

  return {
    getAccessToken: () => accessToken,
    setPair(pair) {
      accessToken = pair.accessToken;
    },
    clear() {
      accessToken = null;
    },
  };
}
