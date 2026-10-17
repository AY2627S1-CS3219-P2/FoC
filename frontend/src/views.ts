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

/**
 * Every view the shell can show. Views are switched by state in App.tsx
 * rather than by URL, since no router is chosen yet (frontend/AGENTS.md).
 */
export const VIEW_NAMES = [
  "home",
  "new-errand",
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
  // AI-generated (edited by nigeltzy).
  /**
   * Listed only for an ADMIN session. Hiding it is an affordance, not access
   * control: user-service refuses an admin action from anyone else.
   */
  adminOnly?: boolean;
}

export const NAV_ITEMS: NavItem[] = [
  { view: "home", label: "Home", enabled: true },
  { view: "new-errand", label: "New errand", enabled: true },
  { view: "my-errands", label: "My errands", enabled: true },
  { view: "suppliers", label: "Suppliers", enabled: true },
  { view: "credits", label: "Credits", enabled: true },
  { view: "profile", label: "Profile", enabled: true },
  // Disabled: nothing publishes notifications yet.
  { view: "notifications", label: "Notifications", enabled: false },
  // AI-generated (edited by nigeltzy).
  { view: "admin", label: "Admin", enabled: true, adminOnly: true },
];
