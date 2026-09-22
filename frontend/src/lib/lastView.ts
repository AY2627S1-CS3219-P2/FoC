// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-22
// Scope: Remembers which view a tab was on, so a page reload lands where the
//   user left off rather than on Home.
// Author review: Nigeltzy - AI built this according to my requirements and the feature works as intended. Tried to keep implementation simple.

import { VIEW_NAMES, type ViewName } from "../views";

/**
 * Where the current tab was last looking.
 *
 * `sessionStorage`, not `localStorage`, and the distinction is the whole
 * point: this is per-TAB. Two tabs on different screens should each come back
 * to their own, and closing a tab should forget it. localStorage would make
 * them fight over one value.
 *
 * This is a view name, never anything sensitive. D-033 keeps tokens out of
 * browser storage; "profile" is a UI convenience and carries nothing an
 * attacker could use.
 *
 * No router is chosen yet (frontend/AGENTS.md), so there is no URL to read
 * this from. When one is, the URL becomes the source of truth and this file
 * goes away — which is why it is one file and not a hook threaded through the
 * tree.
 */
const KEY = "foc.lastView";

/** Reads the stored view, or null if there is nothing usable. */
export function readLastView(): ViewName | null {
  let stored: string | null = null;
  try {
    stored = sessionStorage.getItem(KEY);
  } catch {
    // Private windows and blocked site data both throw on access rather than
    // returning null. Forgetting the view is not worth a blank page.
    return null;
  }
  // Never trust it blindly: the stored value outlives deploys, so a view that
  // has since been renamed or removed would otherwise render nothing at all.
  return VIEW_NAMES.includes(stored as ViewName) ? (stored as ViewName) : null;
}

/** Records the view. Failures are ignored — this is a convenience. */
export function writeLastView(view: ViewName): void {
  try {
    sessionStorage.setItem(KEY, view);
  } catch {
    /* storage unavailable; the tab just forgets where it was */
  }
}

/** Forgets it, so the next sign-in starts at Home. */
export function clearLastView(): void {
  try {
    sessionStorage.removeItem(KEY);
  } catch {
    /* nothing to do */
  }
}
