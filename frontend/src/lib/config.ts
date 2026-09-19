// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-17
// Scope: Central read of build-time configuration.
// Author review: PENDING — <reviewer to complete>

/**
 * Every environment variable the app reads, resolved once here and imported
 * from this module rather than read inline. Mirrors the backend convention of
 * loading config in one place and passing it down (root AGENTS.md §4.4).
 *
 * One base URL per backend service, named <SERVICE>_BASE_URL with the VITE_
 * prefix Vite requires (root AGENTS.md §3). As services land, add a line here.
 *
 * Vite inlines these into the browser bundle at build time, so nothing secret
 * can live behind a VITE_ prefix (frontend/AGENTS.md, Gotchas).
 */
export const config = {
  supplierBaseUrl:
    import.meta.env.VITE_SUPPLIER_BASE_URL ?? "http://localhost:8082",
} as const;
