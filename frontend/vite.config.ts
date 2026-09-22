// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-17
// Scope: Generated the Vite config for the React + TypeScript frontend.
//   2026-09-22: added the same-origin dev proxy D-033 requires.
// Author review: Nigeltzy - Boilerplate vite.config.ts file for ts. No changes needed.

import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Port 3001 is the frontend's allocation in root AGENTS.md §3. In Compose the
// built assets are served by nginx on :80 and published onto 3001; this port
// only applies to `npm run dev`.
//
// THE PROXY IS WHAT MAKES D-033 WORK. The refresh token is an HttpOnly cookie
// with SameSite=Strict, and the browser must see one origin for it to ride
// along at all. Everything under /api and /auth is forwarded to the gateway,
// so the page only ever addresses :3001 — no CORS, no preflight, and no
// `credentials: "include"` anywhere in the app.
//
// The target is localhost:8080 because VITE runs it, on the host. Inside
// Compose the gateway is api-gateway:8080, but this path is `npm run dev`
// only; the built bundle is served by the gateway itself in Compose.
export default defineConfig({
  plugins: [react()],
  server: {
    port: 3001,
    proxy: {
      "/api": { target: "http://localhost:8080", changeOrigin: false },
      "/auth": { target: "http://localhost:8080", changeOrigin: false },
    },
  },
});
