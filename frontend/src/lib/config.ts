// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-19
// Scope: Central read of build-time configuration. Reworked for the API
//   Gateway decision (ai/decisions.md D-010).
//   2026-09-22: VITE_SUPPLIER_BASE_URL removed — the browser now reaches
//   supplier-service through the gateway, which closes D-025b. Later that
//   day: D-033 makes the gateway same-origin, so an EMPTY base URL became the
//   correct production value and could no longer mean "run the fixtures".
// Author review: PENDING — <reviewer to complete>

/**
 * Every environment variable the app reads, resolved once here and imported
 * from this module rather than read inline. Mirrors the backend convention of
 * loading config in one place and passing it down (root AGENTS.md §4.4).
 *
 * Vite inlines these into the browser bundle at build time, so nothing secret
 * can live behind a VITE_ prefix (frontend/AGENTS.md, Gotchas). That rule bites
 * harder now: JWT_SECRET belongs to user-service and the gateway, and must
 * never appear in this folder in any form.
 *
 * D-010 makes the API Gateway the only publicly reachable process, and as of
 * 2026-09-22 the browser holds exactly ONE base URL: VITE_SUPPLIER_BASE_URL is
 * gone and suppliersApi goes through the gateway like everything else. D-015
 * already put the refresh exchange through it, so there is no second URL for
 * auth either.
 */
export const config = {
  /**
   * The gateway (D-010).
   *
   * EMPTY IS THE NORMAL VALUE and means "same origin": requests go to
   * relative paths like /auth/login. D-033 serves the page and the API from
   * one origin — the gateway serves the built SPA in Compose, and Vite's
   * server.proxy does it in development — which is what lets the
   * SameSite=Strict refresh cookie ride along at all.
   *
   * Set it only to aim a dev build at a gateway somewhere else. That makes
   * requests cross-origin again, and the cookie will not be sent.
   */
  gatewayBaseUrl: import.meta.env.VITE_GATEWAY_BASE_URL ?? "",

  /**
   * Run the in-browser fixtures instead of a real gateway. OPT-IN.
   *
   * It has to be explicit. Until 2026-09-22 this was inferred from an empty
   * gateway URL, and D-033 turned that into the correct production value — so
   * the inference silently put the whole Compose stack on mock accounts, and
   * the login page said so while nobody was reading it. A switch that decides
   * whether the app talks to a real backend is not something to derive.
   */
  useFixtures: import.meta.env.VITE_USE_FIXTURES === "true",
} as const;
