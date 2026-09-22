// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-19
// Scope: Central read of build-time configuration. Reworked for the API
//   Gateway decision (ai/decisions.md D-010).
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
 * D-010 makes the API Gateway the only publicly reachable process, so the
 * browser should end up holding exactly ONE base URL. D-015 puts the refresh
 * exchange through it too, so there is no second URL for auth either.
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

  /**
   * TRANSITIONAL — delete this when the gateway actually forwards.
   *
   * D-010 says the browser must not address a service directly.
   *
   * CORRECTED 2026-09-22: this used to say `api-gateway/internal/proxy` was an
   * empty scaffold. It is not — the gateway proxies `/api/suppliers/*` onto
   * supplier-service's own `/suppliers` prefix and has done since PR #1 landed
   * that router on main. What still keeps this line alive is only that nothing
   * has repointed `suppliersApi.ts` at the gateway yet. Doing so, and deleting
   * the published 8082 from `compose.yaml`, is what closes D-025b.
   */
  supplierBaseUrl:
    import.meta.env.VITE_SUPPLIER_BASE_URL ?? "http://localhost:8082",
} as const;
