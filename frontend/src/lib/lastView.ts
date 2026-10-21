// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-22
// Scope: Remembers which view a tab was on, so a page reload lands where the
//   user left off rather than on Home.
// Author review: Nigeltzy - AI built this according to my requirements and the feature works as intended. Tried to keep implementation simple.

import { VIEW_NAMES, type ViewName } from "../views";
// AI-generated (edited by James Teo): the acting mode is remembered beside the
// view, so a reload on a courier screen stays in Delivering mode.
import type { ActingMode } from "../features/auth/types";

/**
 * The view this tab was last on. sessionStorage keeps it per tab and forgets
 * it when the tab closes. Only a view name is stored, never a token.
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

// AI-generated (edited by James Teo): also forgets the acting mode.
/** Forgets it and the acting mode, so the next sign-in starts at Home, Requesting. */
export function clearLastView(): void {
  try {
    sessionStorage.removeItem(KEY);
    sessionStorage.removeItem(MODE_KEY);
  } catch {
    /* nothing to do */
  }
}

// AI-generated (edited by James Teo): the remembered acting mode.

/**
 * The acting mode this tab was last in, kept for the same reason as the view:
 * a reload on Find errands would otherwise come back in Requesting mode,
 * which does not offer that view. Only the mode's name is stored.
 */
const MODE_KEY = "foc.actingMode";

const ACTING_MODES: readonly ActingMode[] = ["requesting", "delivering"];

/** Reads the stored acting mode, or null if there is nothing usable. */
export function readLastMode(): ActingMode | null {
  let stored: string | null = null;
  try {
    stored = sessionStorage.getItem(MODE_KEY);
  } catch {
    return null;
  }
  return ACTING_MODES.includes(stored as ActingMode) ? (stored as ActingMode) : null;
}

/** Records the acting mode. Failures are ignored, as for the view. */
export function writeLastMode(mode: ActingMode): void {
  try {
    sessionStorage.setItem(MODE_KEY, mode);
  } catch {
    /* storage unavailable; a reload starts in Requesting mode */
  }
}
