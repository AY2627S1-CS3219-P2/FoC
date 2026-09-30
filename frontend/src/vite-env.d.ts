// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-17
// Scope: Types the one build-time env var this app reads.
// Author review: Nigeltzy - Boilerplate vite-env.d.ts file for ts. No changes needed.

/// <reference types="vite/client" />

interface ImportMetaEnv {
  /**
   * Origin of the API Gateway. Empty sends API calls to the page's own origin.
   * Inlined into the bundle at build time, so nothing secret may go here.
   */
  readonly VITE_GATEWAY_BASE_URL?: string;
  /** `"true"` runs the in-browser fixtures instead of calling a gateway. */
  readonly VITE_USE_FIXTURES?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
