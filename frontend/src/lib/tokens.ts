// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-19
// Scope: Client-side access/refresh token store, implementing the token
//   lifecycle recorded in ai/decisions.md D-011 and D-015.
//   2026-09-21: dropped setAccessToken — user-service rotates RTs.
//   2026-09-22: the refresh token left this file entirely. D-033 puts it in
//   an HttpOnly cookie the gateway owns, which JS cannot read.
// Author review: Nigeltzy - Used to create the original boilerplate generation, then afterwards
// it was used to update the changes based on my updated requirements, seems to have validly
// made the updates and shifted the relevant code to the gateway which I have also made.

/**
 * Holds the access token in memory only, in a closure. It is never written to
 * localStorage, sessionStorage or a cookie (D-033 in ai/decisions.md); stored
 * there, it would outlive the page and be readable by any script on the origin.
 *
 * The refresh token never reaches this code: it is an HttpOnly cookie the
 * gateway sets. After a reload the store is empty until that cookie is
 * exchanged for a new access token.
 *
 * Created once in App.tsx and passed down, never held in a module variable.
 */

/**
 * What the browser receives from login and refresh. The gateway keeps the
 * refresh token in its cookie, so only the access token arrives.
 */
export interface TokenPair {
  accessToken: string;
}

export interface TokenStore {
  /** The current access token, or null when there is none in memory. */
  getAccessToken(): string | null;
  /** Replaces the access token, after login and after each refresh. */
  setPair(pair: TokenPair): void;
  /**
   * Drops the access token. The session lives on in the refresh cookie, which
   * only a successful logout clears; the next authorized request exchanges
   * the cookie for a new token.
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
