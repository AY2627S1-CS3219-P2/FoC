// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-17
// Scope: Placeholder session types for the mock user-service.
// Author review: PENDING — <reviewer to complete>

/**
 * MOCK SHAPE — invented by the frontend, not a contract. `user-service` does
 * not exist; its owner defines the real identity payload (root AGENTS.md §1).
 * See src/lib/mock.ts.
 */
export interface Session {
  userId: string;
  name: string;
  /** Rendered in the avatar chip, e.g. "NT". */
  initials: string;
  email: string;
}

/**
 * Which side of an errand the user is currently acting as. Two named modes
 * rather than a boolean, per the control-coupling rule in root AGENTS.md §5.
 */
export type ActingMode = "requesting" | "delivering";
