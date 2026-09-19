// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-17
// Scope: Generated the Vite config for the React + TypeScript frontend.
// Author review: PENDING — <reviewer to complete>

import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Port 3001 is the frontend's allocation in root AGENTS.md §3. In Compose the
// built assets are served by nginx on :80 and published onto 3001; this port
// only applies to `npm run dev`.
export default defineConfig({
  plugins: [react()],
  server: { port: 3001 },
});
