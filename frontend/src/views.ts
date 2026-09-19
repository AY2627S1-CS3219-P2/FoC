// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-17
// Scope: View identifiers and the navigation model for the shell.
// Author review: PENDING — <reviewer to complete>

/**
 * No router is chosen yet (frontend/AGENTS.md), so views are switched by state
 * rather than by URL. Keeping the names and the nav model in one place means
 * adopting a router later touches this file and App.tsx, not every component.
 */
export const VIEW_NAMES = [
  "home",
  "new-errand",
  "my-errands",
  "suppliers",
  "credits",
  "profile",
  "notifications",
] as const;

export type ViewName = (typeof VIEW_NAMES)[number];

export interface NavItem {
  view: ViewName;
  label: string;
  /** False while the backing service does not exist at all. */
  enabled: boolean;
  /** Shown in the mobile tab bar, which holds five items. */
  inTabBar: boolean;
}

export const NAV_ITEMS: NavItem[] = [
  { view: "home", label: "Home", enabled: true, inTabBar: true },
  { view: "new-errand", label: "New errand", enabled: true, inTabBar: true },
  { view: "my-errands", label: "My errands", enabled: true, inTabBar: true },
  { view: "suppliers", label: "Suppliers", enabled: true, inTabBar: false },
  { view: "credits", label: "Credits", enabled: true, inTabBar: true },
  { view: "profile", label: "Profile", enabled: true, inTabBar: true },
  // Nothing publishes notifications yet, so this stays visibly inert rather
  // than showing an invented feed.
  { view: "notifications", label: "Notifications", enabled: false, inTabBar: false },
];
