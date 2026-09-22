// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-19
// Scope: Central read of build-time configuration. Reworked for the API
//   Gateway decision (ai/decisions.md D-010).
//   2026-09-22: VITE_SUPPLIER_BASE_URL removed — the browser now reaches
//   supplier-service through the gateway, which closes D-025b.
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
   * The gateway (D-010), on its provisional port 8080 (D-018).
   *
   * App.tsx reads an empty value as "no gateway, run the fixtures", so the
   * default here is what switches the app onto the real auth flow. It stays
   * empty until api-gateway can actually serve those routes — set
   * VITE_GATEWAY_BASE_URL in .env to try it against a running gateway.
   */
  gatewayBaseUrl: import.meta.env.VITE_GATEWAY_BASE_URL ?? "",
} as const;
