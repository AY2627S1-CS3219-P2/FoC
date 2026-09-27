// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-19
// Scope: Central read of build-time configuration. Reworked for the API
//   Gateway decision (ai/decisions.md D-010).
//   2026-09-22: VITE_SUPPLIER_BASE_URL removed — the browser now reaches
//   supplier-service through the gateway, which closes D-025b. Later that
//   day: D-033 makes the gateway same-origin, so an EMPTY base URL became the
//   correct production value and could no longer mean "run the fixtures".
// Author review: Nigeltzy - Used to create boilerplate config implementation,
// seems like a typical implementation of config.ts, and the changes made to the file are valid and correct based on the updated requirements.

/**
 * Every environment variable the app reads, resolved once here and imported
 * from this module rather than read inline.
 *
 * Vite inlines these into the browser bundle at build time, so nothing secret
 * may go in a VITE_ variable.
 */
export const config = {
  /**
   * Base URL put in front of every API path. Empty (the default) means
   * relative paths like /auth/login on the page's own origin.
   *
   * fetch() never sets `credentials` here, so the refresh cookie only travels
   * when the page and the API share an origin. Pointed at another origin, the
   * cookie is not sent.
   */
  gatewayBaseUrl: import.meta.env.VITE_GATEWAY_BASE_URL ?? "",

  /**
   * Run the in-browser fixtures instead of the gateway. Only the exact value
   * "true" turns them on.
   */
  useFixtures: import.meta.env.VITE_USE_FIXTURES === "true",
} as const;
