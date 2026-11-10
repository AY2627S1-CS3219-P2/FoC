// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-17
// Scope: View identifiers and the navigation model for the shell.
// Author review: Nigeltzy - Boilerplate view template for ts.
//
// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5), date: 2026-10-02
// Scope: Removed NavItem.inTabBar — the bottom tab bar it gated is gone, the
//   sidebar nav is now used at every width.
// Author review: nigeltzy — Checked that the component was updated according to 
// the new format and requirements.

// AI-generated (edited by James Teo): ActingMode and AccountRole, for the
// nav's per-mode and per-role items.
import type { AccountRole, ActingMode } from "./features/auth/types";

/**
 * Every view the shell can show. Views are switched by state in App.tsx
 * rather than by URL, since no router is chosen yet (frontend/AGENTS.md).
 */
export const VIEW_NAMES = [
  "new-errand",
  // AI-generated (edited by James Teo): the courier's open list.
  "find-errands",
  "my-errands",
  "suppliers",
  "credits",
  "profile",
  "notifications",
  // AI-generated (edited by nigeltzy).
  "admin",
] as const;

export type ViewName = (typeof VIEW_NAMES)[number];

export interface NavItem {
  view: ViewName;
  label: string;
  /** False while the backing service does not exist at all. */
  enabled: boolean;
  // AI-generated (edited by James Teo): `audience` replaces `adminOnly`, and
  // `modes` puts the requester's and courier's items under the acting-mode
  // toggle.
  /**
   * Listed only for this account role; for both when absent. Hiding an item
   * is an affordance, not access control: each service refuses what the
   * caller's role does not allow.
   */
  audience?: AccountRole;
  /**
   * Listed for a STUDENT session only in these acting modes; in both when
   * absent. An ADMIN session has no acting mode, so this does not apply to it.
   */
  modes?: ActingMode[];
}

// AI-generated (edited by James Teo): Find errands and All errands, and each
// item's audience and modes. An ADMIN session's nav starts with its own
// tool, the Dashboard; a STUDENT session never lists it.
export const NAV_ITEMS: NavItem[] = [
  // AI-generated (edited by nigeltzy).
  // AI-generated (edited by James Teo): `audience` in place of `adminOnly`.
  // AI-generated (edited by Claude Code, 2026-11-03): the admin dashboard
  // (D-101 to D-123) holds the screens "All errands" and "Admin" were, and the
  // accounts, past errands and audit log. The view keeps the name "admin" so a
  // remembered view still lands on it.
  { view: "admin", label: "Dashboard", enabled: true, audience: "ADMIN" },
  {
    view: "new-errand",
    label: "New errand",
    enabled: true,
    audience: "STUDENT",
    modes: ["requesting"],
  },
  {
    view: "find-errands",
    label: "Find errands",
    enabled: true,
    audience: "STUDENT",
    modes: ["delivering"],
  },
  { view: "my-errands", label: "My errands", enabled: true, audience: "STUDENT" },
  { view: "suppliers", label: "Suppliers", enabled: true },
  { view: "credits", label: "Credits", enabled: true, audience: "STUDENT" },
  { view: "profile", label: "Profile", enabled: true },
  // AI-generated (edited by nigeltzy): notification-service serves it (D-145).
  { view: "notifications", label: "Notifications", enabled: true },
];

// AI-generated (edited by James Teo): one rule for both the nav and App's view
// guard.
/**
 * Whether `view` belongs to this session's nav: its role, and for a STUDENT
 * the acting mode. AppShell lists exactly these views (a disabled one greyed
 * out), and App falls back to the session's default view for any other, such
 * as a remembered view the session can no longer see. Presentation only; the services enforce
 * every rule.
 */
export function isViewAvailable(view: ViewName, role: AccountRole, mode: ActingMode): boolean {
  const item = NAV_ITEMS.find((candidate) => candidate.view === view);
  if (!item) return false;
  if (item.audience && item.audience !== role) return false;
  if (role === "STUDENT" && item.modes && !item.modes.includes(mode)) return false;
  return true;
}

// AI-generated (edited by Claude Code, 2026-11-09): there is no Home page; a
// session opens on the screen it works from (D-154).
/**
 * Where a session lands when no view is remembered or the remembered one is not
 * its own: My errands for a student, the Dashboard for an admin.
 */
export function defaultView(role: AccountRole): ViewName {
  return role === "ADMIN" ? "admin" : "my-errands";
}
