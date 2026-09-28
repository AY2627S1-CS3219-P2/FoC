// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-17
// Scope: Generated the Vite config for the React + TypeScript frontend.
//   2026-09-22: added the same-origin dev proxy D-033 requires.
// Author review: Nigeltzy - Boilerplate vite.config.ts file for ts. No changes needed.

import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// `npm run dev` only: serves the app on port 3001 (the frontend's port in root
// AGENTS.md §3) and forwards /api and /auth to the gateway on localhost:8080,
// which compose.yaml publishes, so the page calls only its own origin and the
// refresh cookie is sent. strictPort stops instead of moving to another port
// when 3001 is taken.
export default defineConfig({
  plugins: [react()],
  server: {
    port: 3001,
    strictPort: true,
    proxy: {
      "/api": { target: "http://localhost:8080", changeOrigin: false },
      "/auth": { target: "http://localhost:8080", changeOrigin: false },
    },
  },
});
