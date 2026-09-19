// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-17
// Scope: Types the one build-time env var this app reads.
// Author review: PENDING — <reviewer to complete>

/// <reference types="vite/client" />

interface ImportMetaEnv {
  /**
   * Base URL of supplier-service. Follows the repo's <SERVICE>_BASE_URL naming
   * with Vite's required VITE_ prefix (root AGENTS.md §3). Nothing secret may
   * go in a VITE_ variable — it is inlined into the bundle the browser gets.
   */
  readonly VITE_SUPPLIER_BASE_URL?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
